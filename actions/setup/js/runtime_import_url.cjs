// @ts-check

const dns = require("node:dns");
const http = require("node:http");
const https = require("node:https");
const { BlockList, isIP } = require("node:net");
const { ERR_API, ERR_VALIDATION } = require("./error_codes.cjs");
const { getErrorMessage } = require("./error_helpers.cjs");

/**
 * Display URL destinations without authentication, query, or fragment data.
 * @param {string | URL} value
 * @returns {string}
 */
function displayImportUrl(value) {
  try {
    const url = new URL(value);
    return `${url.origin}${url.pathname}`;
  } catch {
    return "(invalid URL)";
  }
}

/**
 * Transport errors can also embed the requested URL in their messages.
 * @param {unknown} error
 * @returns {string}
 */
function importUrlErrorMessage(error) {
  return getErrorMessage(error).replace(/https?:\/\/[^\s"'<>]+/gi, displayImportUrl);
}

const nonPublicIPv4 = new BlockList();
for (const [address, prefix] of [
  ["0.0.0.0", 8],
  ["10.0.0.0", 8],
  ["100.64.0.0", 10],
  ["127.0.0.0", 8],
  ["169.254.0.0", 16],
  ["172.16.0.0", 12],
  ["192.0.0.0", 24],
  ["192.0.2.0", 24],
  ["192.88.99.0", 24],
  ["192.168.0.0", 16],
  ["198.18.0.0", 15],
  ["198.51.100.0", 24],
  ["203.0.113.0", 24],
  ["224.0.0.0", 3],
]) {
  nonPublicIPv4.addSubnet(String(address), Number(prefix), "ipv4");
}
// Azure's platform virtual IP is publicly numbered but reaches runner-local services.
nonPublicIPv4.addAddress("168.63.129.16", "ipv4");

const globalIPv6 = new BlockList();
globalIPv6.addSubnet("2000::", 3, "ipv6");
const nonPublicIPv6 = new BlockList();
for (const [address, prefix] of [
  ["2001::", 23], // Special-purpose space, including Teredo and benchmarking.
  ["2001:db8::", 32],
  ["2002::", 16], // 6to4 can encode a non-public IPv4 destination.
  ["3fff::", 20],
]) {
  nonPublicIPv6.addSubnet(String(address), Number(prefix), "ipv6");
}

/**
 * Only ordinary public unicast addresses are import destinations. IPv4-mapped,
 * NAT64, and other translation/tunneling ranges are deliberately not capabilities.
 * @param {string} address
 * @returns {boolean}
 */
function isPublicImportAddress(address) {
  const family = isIP(address);
  if (family === 4) return !nonPublicIPv4.check(address, "ipv4");
  return family === 6 && globalIPv6.check(address, "ipv6") && !nonPublicIPv6.check(address, "ipv6");
}

/**
 * @param {string} value
 * @returns {URL}
 */
function validateImportUrl(value) {
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new Error(`${ERR_VALIDATION}: Runtime import URL must be a valid HTTP or HTTPS URL`);
  }
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) {
    throw new Error(`${ERR_VALIDATION}: Runtime import URL requires unauthenticated HTTP or HTTPS`);
  }
  const hostname = url.hostname.replace(/^\[|\]$/g, "");
  if (isIP(hostname) && !isPublicImportAddress(hostname)) {
    throw new Error(`${ERR_VALIDATION}: Runtime import URL destination must be a public IP address`);
  }
  return url;
}

/**
 * Resolve once, validate every answer, and give those same addresses to the socket.
 * A separate preflight DNS check followed by ordinary fetch would permit rebinding.
 * @type {import("node:net").LookupFunction}
 */
function lookupPublicImport(hostname, options, callback) {
  dns.lookup(hostname, { all: true, verbatim: true }, (error, addresses) => {
    if (error) {
      callback(error, "", 4);
      return;
    }
    if (!addresses.length || addresses.some(({ address }) => !isPublicImportAddress(address))) {
      callback(new Error(`${ERR_VALIDATION}: Runtime import URL destination must resolve only to public IP addresses`), "", 4);
      return;
    }
    if (options.all) {
      callback(null, addresses);
    } else {
      callback(null, addresses[0].address, addresses[0].family);
    }
  });
}

/**
 * Requests run on the runner, not in the agent firewall. Use a fresh direct
 * connection with a validating DNS lookup; preserve the hostname for Host/SNI
 * and certificate verification. Node HTTP does not follow redirects.
 * @param {URL} url
 * @param {AbortSignal} signal
 * @returns {Promise<string>}
 */
async function readPublicImportUrl(url, signal) {
  try {
    const transport = url.protocol === "https:" ? https : http;
    const response = await new Promise((resolve, reject) => {
      const request = transport.get(url, { agent: false, lookup: lookupPublicImport, signal }, resolve);
      request.on("error", reject);
    });
    const status = response.statusCode || 0;
    if (status < 200 || status >= 300) {
      response.destroy();
      throw new Error(`${ERR_API}: Failed to fetch URL ${displayImportUrl(url)}: HTTP ${status}`);
    }
    const chunks = [];
    for await (const chunk of response) {
      chunks.push(Buffer.from(chunk));
    }
    return Buffer.concat(chunks).toString("utf8");
  } catch (error) {
    if (error instanceof Error && error.message.startsWith(ERR_API)) {
      throw new Error(importUrlErrorMessage(error));
    }
    throw new Error(`${ERR_API}: Failed to fetch URL ${displayImportUrl(url)}: ${importUrlErrorMessage(error)}`);
  }
}

module.exports = { displayImportUrl, importUrlErrorMessage, isPublicImportAddress, validateImportUrl, readPublicImportUrl };
