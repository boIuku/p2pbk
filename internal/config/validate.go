package config

// This file intentionally owns validation-related package boundaries.
// The first stage keeps the implementation on Config.Validate; later stages can
// add focused validators here as networking, identity, and storage mature.
