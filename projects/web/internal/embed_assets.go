package app

import "embed"

// Vite produces index.html and flat, content-hashed assets. Keeping the built
// files here also permits Go-only builds without a local Node.js installation.
//
//go:embed assets/*
var assets embed.FS
