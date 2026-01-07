// Package decor provides common decorators for "github.com/vbauerster/mpb/v8" module.
//
// Some decorators returned by this package might have a closure state. It is ok to use
// decorators concurrently, unless you share the same decorator among multiple
// *mpb.Bar instances. To avoid data races, create new decorator per *mpb.Bar instance.
//
// Don't:
//
//	p := mpb.New()
//	name := decor.Name("bar")
//	p.AddBar(100, mpb.AppendDecorators(name))
//	p.AddBar(100, mpb.AppendDecorators(name))
//
// Do:
//
//	p := mpb.New()
//	p.AddBar(100, mpb.AppendDecorators(decor.Name("bar1")))
//	p.AddBar(100, mpb.AppendDecorators(decor.Name("bar2")))
package decor
