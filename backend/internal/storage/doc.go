// Package storage persists domain.AppState in either a JSON file
// (development, tests, migration source) or PostgreSQL (deployment). Callers
// declare a Scope describing the rows they touch; the PostgreSQL backend loads
// and locks only those rows.
package storage
