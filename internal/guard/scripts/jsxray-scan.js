#!/usr/bin/env node
// jsxray-scan.js — reads JavaScript source from stdin, runs JS-X-Ray analysis,
// outputs JSON warnings to stdout.
// Usage: echo "const x = eval(atob('...'))" | node jsxray-scan.js

import { AstAnalyser } from "@nodesecure/js-x-ray";

const chunks = [];
for await (const chunk of process.stdin) {
    chunks.push(chunk);
}
const source = Buffer.concat(chunks).toString("utf8");

if (!source.trim()) {
    process.stdout.write(JSON.stringify({ warnings: [], dependencies: [] }));
    process.exit(0);
}

try {
    const scanner = new AstAnalyser();
    const result = scanner.analyse(source);
    const output = {
        warnings: (result.warnings || []).map(w => ({
            kind: w.kind || "unknown",
            value: w.value || "",
            location: (w.location && w.location.start) ? `${w.location.start.line}:${w.location.start.column}` : "",
        })),
        dependencies: Object.keys(result.dependencies || {}),
    };
    process.stdout.write(JSON.stringify(output));
} catch (err) {
    process.stdout.write(JSON.stringify({
        warnings: [],
        dependencies: [],
        error: err.message,
    }));
}

