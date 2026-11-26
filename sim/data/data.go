// Package data embeds the hand-written game facts the DB2 tables cannot
// provide. Every entry in these files carries a "source" note; "Classic
// value" marks numbers that are not yet measured on Forever.
package data

import _ "embed"

//go:embed overrides.json
var Overrides []byte

//go:embed targets.json
var Targets []byte

//go:embed threat.json
var Threat []byte

//go:embed bis_sources.json.gz
var BiSSources []byte
