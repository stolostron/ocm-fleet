"use strict";

const dns = require("node:dns");
const lookup = dns.lookup.bind(dns);
const promiseLookup = dns.promises.lookup.bind(dns.promises);

function isLocalhostSubdomain(hostname) {
  return typeof hostname === "string" && hostname.endsWith(".localhost");
}

dns.lookup = (hostname, options, callback) => {
  if (typeof options === "function") {
    callback = options;
    options = {};
  }
  if (!isLocalhostSubdomain(hostname)) {
    return lookup(hostname, options, callback);
  }
  if (options?.all) {
    return process.nextTick(callback, null, [
      { address: "127.0.0.1", family: 4 },
    ]);
  }
  return process.nextTick(callback, null, "127.0.0.1", 4);
};

dns.promises.lookup = async (hostname, options) => {
  if (!isLocalhostSubdomain(hostname)) {
    return promiseLookup(hostname, options);
  }
  if (options?.all) return [{ address: "127.0.0.1", family: 4 }];
  return { address: "127.0.0.1", family: 4 };
};
