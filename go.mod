module github.com/nicolagi/telegramfs

require (
	github.com/boltdb/bolt v1.3.1
	github.com/google/go-cmp v0.3.1
	github.com/lionkov/go9p v0.0.0-20190125202718-b4200817c487
	golang.org/x/sys v0.0.0-20201101102859-da207088b7d1 // indirect
)

go 1.13

replace github.com/lionkov/go9p v0.0.0-20190125202718-b4200817c487 => github.com/nicolagi/go9p v0.0.0-20190223213930-d791c5b05663
