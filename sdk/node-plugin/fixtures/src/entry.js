import("./host.js").catch((error) => {
  console.error(error);
  process.exitCode = 1;
});
