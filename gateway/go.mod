module github.com/AsyncAssassin/go-homework/gateway

go 1.22

require github.com/AsyncAssassin/go-homework/ledger v0.0.0-00010101000000-000000000000

// Gateway uses the ledger module from this repository, not a published one.
replace github.com/AsyncAssassin/go-homework/ledger => ../ledger
