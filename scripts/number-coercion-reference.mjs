// Offline language-runtime reference; no cloud or device access.
const inputs = [
  "0x10000000000000000",
  "0o2000000000000000000000",
  "0b1" + "0".repeat(64),
  "0x20000000000001",
  "0x20000000000003",
  "0x" + "f".repeat(256),
  "0x1" + "0".repeat(256),
  "0x" + "f".repeat(40) + "z",
  "0o" + "7".repeat(40) + "8",
  "0b" + "1".repeat(80) + "2",
  "0x+1",
  "0x-1",
  "0x1_0",
  "0x",
  "0b",
  "0o",
  "  0x10  ",
  "+0x10",
  "-0x10",
  "Infinity",
  "-Infinity",
  "",
  " ",
  "1000ms",
  "1e-7",
];
const describe = (v) =>
  Number.isNaN(v)
    ? { kind: "nan" }
    : Number.isFinite(v)
      ? { kind: "finite", value: v }
      : { kind: v > 0 ? "positive-infinity" : "negative-infinity" };
console.log(
  JSON.stringify(
    {
      runtime: process.version,
      source: "JavaScript Number and parseInt; not official cloud observations",
      cases: inputs.map((input) => ({
        input,
        number: describe(Number(input)),
        csv: describe(parseInt(input)),
      })),
    },
    null,
    2,
  ),
);
