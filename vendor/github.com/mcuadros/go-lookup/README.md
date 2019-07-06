go-lookup [![Build Status](https://travis-ci.org/mcuadros/go-lookup.png?branch=master)](https://travis-ci.org/mcuadros/go-lookup) [![GoDoc](http://godoc.org/github.com/mcuadros/go-lookup?status.png)](http://godoc.org/github.com/mcuadros/go-lookup)
==============================

Small library on top of reflect for make lookups to Structs or Maps. Using a very simple DSL you can access to any property, key or value of any value of Go.

Installation
------------

The recommended way to install go-lookup

```
go get github.com/mcuadros/go-lookup
```

Example
-------

```go
type Cast struct {
  Actor, Role string
}

type Serie struct {
  Cast []Cast
}

series := map[string]Serie{
  "A-Team": {Cast: []Cast{
    {Actor: "George Peppard", Role: "Hannibal"},
    {Actor: "Dwight Schultz", Role: "Murdock"},
    {Actor: "Mr. T", Role: "Baracus"},
    {Actor: "Dirk Benedict", Role: "Faceman"},
  }},
}

q := "A-Team.Cast.Role"
value, _ := LookupString(series, q)
fmt.Println(q, "->", value.Interface())
// A-Team.Cast.Role -> [Hannibal Murdock Baracus Faceman]

q = "A-Team.Cast[0].Actor"
value, _ = LookupString(series, q)
fmt.Println(q, "->", value.Interface())
// A-Team.Cast[0].Actor -> George Peppard
```

License
-------

MIT, see [LICENSE](LICENSE)
