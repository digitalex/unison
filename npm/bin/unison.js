#!/usr/bin/env node
"use strict";

const { main } = require("../lib/install.js");

main(process.argv.slice(2)).catch((err) => {
  console.error(`unison-cli: ${err.message}`);
  process.exit(1);
});
