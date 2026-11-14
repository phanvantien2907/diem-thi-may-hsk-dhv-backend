package data

import _ "embed"

// V1SnapshotJSON holds the offline snapshot of provinces.open-api.vn v1 (/api/?depth=3).
//
//go:embed v1_snapshot.json
var V1SnapshotJSON []byte

// V2ProvincesSnapshotJSON holds the offline snapshot of provinces.open-api.vn v2 provinces (/api/v2/p/?depth=3).
//
//go:embed v2_provinces_snapshot.json
var V2ProvincesSnapshotJSON []byte

// V2WardsSnapshotJSON holds the offline snapshot of provinces.open-api.vn v2 wards (/api/v2/w/).
//
//go:embed v2_wards_snapshot.json
var V2WardsSnapshotJSON []byte

