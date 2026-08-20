const allowedLicenses = new Set([
  "Apache-2.0",
  "BlueOak-1.0.0",
  "BSD-2-Clause",
  "BSD-3-Clause",
  "CC-BY-4.0",
  "CC0-1.0",
  "ISC",
  "MIT",
  "MIT-0",
]);

let input = "";
for await (const chunk of process.stdin) {
  input += chunk;
}
if (!input.trim()) {
  throw new Error("license inventory is empty");
}

const inventory = JSON.parse(input);
const observedLicenses = Object.keys(inventory).sort();
const unapproved = observedLicenses.filter(
  (license) => !allowedLicenses.has(license),
);
if (unapproved.length > 0) {
  throw new Error(`unapproved Node license classes: ${unapproved.join(", ")}`);
}

function requireExactPackage(license, expectedName) {
  const packages = inventory[license] ?? [];
  if (packages.length !== 1 || packages[0].name !== expectedName) {
    const names = packages.map((entry) => entry.name).sort().join(", ");
    throw new Error(
      `${license} dependencies changed: expected ${expectedName}, observed ${names || "none"}`,
    );
  }
  const entry = packages[0];
  if (
    !Array.isArray(entry.versions) ||
    entry.versions.length === 0 ||
    !entry.homepage
  ) {
    throw new Error(`${expectedName} is missing attribution metadata`);
  }
}

requireExactPackage("CC-BY-4.0", "caniuse-lite");
requireExactPackage("CC0-1.0", "mdn-data");

const packageCount = observedLicenses.reduce(
  (total, license) => total + inventory[license].length,
  0,
);
console.log(
  `node-license-policy: PASS packages=${packageCount} classes=${observedLicenses.join(",")}`,
);
