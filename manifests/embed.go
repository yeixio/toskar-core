package manifests

import _ "embed"

//go:embed models/catalog.json
var CatalogJSON []byte

//go:embed models/presets.json
var PresetsJSON []byte

// MediaJSON lists the picture and clip models setup offers (#551).
//
//go:embed models/media.json
var MediaJSON []byte

// CategoriesJSON is the store's taxonomy: what models do, as categories
// and tasks, and the kinds of input and output they take (#593).
//
//go:embed models/categories.json
var CategoriesJSON []byte
