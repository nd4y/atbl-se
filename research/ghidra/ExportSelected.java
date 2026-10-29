// Export selected existing functions and their direct callees from the saved inert program.
import ghidra.app.script.GhidraScript;
import ghidra.app.decompiler.*;
import ghidra.program.model.address.*;
import ghidra.program.model.listing.*;
import ghidra.program.model.symbol.*;
import java.nio.file.*;
import java.util.*;
public class ExportSelected extends GhidraScript {
 public void run() throws Exception {
  String[] args=getScriptArgs();Path out=Paths.get(args[0]);Files.createDirectories(out);
  // A non-returning restart call was followed through padding at 0x400d8f97.
  // Native call8 targets and aligned ENTRY bytes independently establish 8f98.
  if(getFunctionAt(toAddr(0x400d8f98L))==null) {
   Function previous=getFunctionContaining(toAddr(0x400d8f98L));
   if(previous!=null)previous.setBody(new AddressSet(previous.getEntryPoint(),toAddr(0x400d8f96L)));
   clearListing(toAddr(0x400d8f97L),toAddr(0x400d9023L));
   disassemble(toAddr(0x400d8f98L));createFunction(toAddr(0x400d8f98L),"local_script_loaded_reply");
  }
  Set<Function> functions=new TreeSet<>((a,b)->a.getEntryPoint().compareTo(b.getEntryPoint()));
  StringBuilder refs=new StringBuilder();
  for(int n=1;n<args.length;n++) {
   if(args[n].startsWith("range:")) {
    String[] bounds=args[n].split(":");long end=Long.parseLong(bounds[2],16);
    FunctionIterator it=currentProgram.getFunctionManager().getFunctions(toAddr(Long.parseLong(bounds[1],16)),true);
    while(it.hasNext()){Function f=it.next();if(f.getEntryPoint().getOffset()>=end)break;functions.add(f);}
   } else if(args[n].startsWith("0x")) {
    Address address=toAddr(Long.parseLong(args[n].substring(2),16));Function f=getFunctionAt(address);
    if(f==null){disassemble(address);f=createFunction(address,null);}
    if(f==null)throw new Exception("Function absent "+args[n]);functions.add(f);
   } else {
    byte[] needle=(args[n]+"\0").getBytes("US-ASCII");Address cursor=currentProgram.getMinAddress();
    while((cursor=currentProgram.getMemory().findBytes(cursor,needle,null,true,monitor))!=null) {
     refs.append(args[n]+" @ "+cursor+"\n");ArrayList<Address> todo=new ArrayList<>();todo.add(cursor);Set<Address> visited=new HashSet<>();
     for(int k=0;k<todo.size()&&k<200;k++) {
      Address target=todo.get(k);if(!visited.add(target))continue;
      for(Reference r:getReferencesTo(target)) {
       Function f=getFunctionContaining(r.getFromAddress());refs.append(r.getFromAddress()+" -> "+target+" function="+(f==null?"literal":f.getEntryPoint())+"\n");
       if(f!=null)functions.add(f);else if(k<50)todo.add(r.getFromAddress());
      }
     }
     cursor=cursor.add(1);
    }
   }
  }
  Set<Function> direct=new HashSet<>();
  for(Function f:functions)for(Function callee:f.getCalledFunctions(monitor))direct.add(callee);
  functions.addAll(direct);Files.writeString(out.resolve("references.txt"),refs.toString());
  DecompInterface d=new DecompInterface();d.openProgram(currentProgram);
  for(Function f:functions) {
   DecompileResults r=d.decompileFunction(f,45,monitor);
   Files.writeString(out.resolve("function_"+f.getEntryPoint()+".c"),r.decompileCompleted()?r.getDecompiledFunction().getC():"ERROR "+r.getErrorMessage());
   StringBuilder listing=new StringBuilder();InstructionIterator it=currentProgram.getListing().getInstructions(f.getBody(),true);
   while(it.hasNext()){Instruction i=it.next();listing.append(i.getAddress()+" "+i+"\n");}
   Files.writeString(out.resolve("function_"+f.getEntryPoint()+".asm"),listing.toString());
  }
  d.dispose();println("SELECTED_EXPORTED count="+functions.size());
 }
}
