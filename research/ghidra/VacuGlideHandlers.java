import ghidra.app.script.GhidraScript;
import ghidra.app.decompiler.*;
import ghidra.program.model.address.*;
import ghidra.program.model.listing.*;
import ghidra.program.model.symbol.*;
import java.nio.file.*;
import ghidra.program.model.mem.MemoryBlock;
import java.io.ByteArrayInputStream;
import java.util.*;
public class VacuGlideHandlers extends GhidraScript {

  private long u32(byte[] b,int p) { return Integer.toUnsignedLong((b[p]&255)|((b[p+1]&255)<<8)|((b[p+2]&255)<<16)|((b[p+3]&255)<<24)); }
  private void mapEsp(String imagePath) throws Exception {
    byte[] b=Files.readAllBytes(Paths.get(imagePath));
    if ((b[0]&255)!=0xe9) throw new Exception("Not ESP app image");
    for (MemoryBlock block:currentProgram.getMemory().getBlocks()) currentProgram.getMemory().removeBlock(block,monitor);
    int p=24;
    for (int n=0;n<(b[1]&255);n++) {
      long va=u32(b,p); int size=(int)u32(b,p+4);p+=8;
      MemoryBlock block=currentProgram.getMemory().createInitializedBlock("esp_segment_"+n,toAddr(va),new ByteArrayInputStream(b,p,size),size,monitor,false);
      block.setRead(true);block.setWrite(va<0x40000000L && va>=0x3ff00000L);block.setExecute(va>=0x40000000L);
      if (block.isExecute()) {
        for (int o=0;o<size-3;o++) {
          if (((va+o)&3)==0 && (b[p+o]&255)==0x36 && (b[p+o+1]&15)==1 && (b[p+o+2]&255)<16) {
            Address a=toAddr(va+o);
            disassemble(a);
            if (getFunctionAt(a)==null) createFunction(a,null);
          }
        }
      }
      p+=size;
    }
    Address entry=toAddr(u32(b,4));
    currentProgram.getSymbolTable().addExternalEntryPoint(entry);
    disassemble(entry);
    if (getFunctionAt(entry)==null) createFunction(entry,"esp_entry");
    println("ESP_SEGMENTS_LOADED functions="+currentProgram.getFunctionManager().getFunctionCount());
  }

  public void run() throws Exception {
    mapEsp(getScriptArgs()[0]);
    String output=getScriptArgs()[1];
    Files.createDirectories(Paths.get(output));
    if(currentProgram.getMemory().getBlock(toAddr(0x400d0020L))==null) throw new Exception("ESP memory mapping absent; abort export");
    analyzeAll(currentProgram);
    String[] commands={"state","valve-stroke-minus-set","valve-stroke-plus-set","target-speed-set","target-speed-stop","local-script-loaded","local-script-move","local-script-replace","local-script-set","local-script-start","local-script-stop","local-script-swap","sync-script-load-token","sync-script-loop","sync-script-offset","sync-script-start","sync-script-stop"};
    Set<Function> functions=new TreeSet<>((a,b)->a.getEntryPoint().compareTo(b.getEntryPoint()));
    FunctionIterator candidates=currentProgram.getFunctionManager().getFunctions(toAddr(0x400d8c88L),true);
    while(candidates.hasNext()){Function f=candidates.next();if(f.getEntryPoint().getOffset()>=0x400d9250L)break;functions.add(f);}
    StringBuilder refs=new StringBuilder();
    for (String command:commands) {
      byte[] needle=(command+"\0").getBytes("US-ASCII");
      Address cursor=currentProgram.getMinAddress();
      while ((cursor=currentProgram.getMemory().findBytes(cursor,needle,null,true,monitor))!=null) {
        refs.append(command+" @ "+cursor+"\n");
        ArrayList<Address> todo=new ArrayList<>();todo.add(cursor);
        Set<Address> visited=new HashSet<>();
        for (int k=0;k<todo.size() && k<200;k++) {
          Address target=todo.get(k);if (!visited.add(target)) continue;
          for (Reference r:getReferencesTo(target)) {
            Address from=r.getFromAddress();Function f=getFunctionContaining(from);
            refs.append("  "+from+" -> "+target+" function="+(f==null?"literal":f.getEntryPoint())+"\n");
            if (f!=null) functions.add(f); else if(k<50) todo.add(from);
          }
        }
        cursor=cursor.add(1);
      }
    }
    Files.writeString(Paths.get(output,"references.txt"),refs.toString());
    DecompInterface decomp=new DecompInterface();decomp.openProgram(currentProgram);
    for (Function f:functions) {
      DecompileResults result=decomp.decompileFunction(f,60,monitor);
      String text=result.decompileCompleted()?result.getDecompiledFunction().getC():"ERROR "+result.getErrorMessage();
      Files.writeString(Paths.get(output,"function_"+f.getEntryPoint()+".c"),text);
      StringBuilder listing=new StringBuilder();InstructionIterator it=currentProgram.getListing().getInstructions(f.getBody(),true);
      while(it.hasNext()){Instruction i=it.next();listing.append(i.getAddress()+" "+i+"\n");}
      Files.writeString(Paths.get(output,"function_"+f.getEntryPoint()+".asm"),listing.toString());
    }
    decomp.dispose();
    println("HANDLERS_EXPORTED count="+functions.size());
  }
}
