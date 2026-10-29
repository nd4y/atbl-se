const values = [
  -1,
  -0.1,
  -1e-7,
  -1e-8,
  255.1,
  256,
  10000,
  1000000000,
  4294967295,
  4294967296,
  4294967296.5,
  1e20,
  1e21,
  Infinity,
  -Infinity,
  NaN,
];
const cases = [];
for (const field of ["time", "position"])
  for (const value of values) {
    const b = Buffer.alloc(4);
    let outcome;
    try {
      if (field === "time") b.writeUInt32LE(value);
      else b.writeUInt8(value);
      outcome = { accepted: true, packed: field === "time" ? b.readUInt32LE() : b.readUInt8() };
    } catch (e) {
      outcome = { accepted: false, code: e.code, message: e.message };
    }
    cases.push({ field, source: Number.isFinite(value) ? value : String(value), ...outcome });
  }
console.log(
  JSON.stringify(
    {
      runtime: process.version,
      reference: "Node Buffer primitive only, not official cloud observations",
      cases,
    },
    null,
    2,
  ),
);
