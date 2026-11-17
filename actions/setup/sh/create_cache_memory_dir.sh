#!/usr/bin/env bash
set +o histexpand
mkdir -p "${GH_AW_RUNTIME_DIR:?}/cache-memory"
echo "Cache memory directory created at ${GH_AW_RUNTIME_DIR:?}/cache-memory"
echo "This folder provides persistent file storage across workflow runs"
echo "LLMs and agentic tools can freely read and write files in this directory"
