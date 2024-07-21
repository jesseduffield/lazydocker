// FIXME(thaJeztah): remove once we are a module; the go:build directive prevents go from downgrading language version to go1.16:
//go:build go1.21

package store

// TypeGetter is a func used to determine the concrete type of a context or
// endpoint metadata by returning a pointer to an instance of the object
// eg: for a context of type DockerContext, the corresponding TypeGetter should return new(DockerContext)
type TypeGetter func() any

// NamedTypeGetter is a TypeGetter associated with a name
type NamedTypeGetter struct {
	name       string
	typeGetter TypeGetter
}

// EndpointTypeGetter returns a NamedTypeGetter with the specified name and getter
func EndpointTypeGetter(name string, getter TypeGetter) NamedTypeGetter {
	return NamedTypeGetter{
		name:       name,
		typeGetter: getter,
	}
}

// Config is used to configure the metadata marshaler of the context ContextStore
type Config struct {
	contextType   TypeGetter
	endpointTypes map[string]TypeGetter
}

// SetEndpoint set an endpoint typing information
func (c Config) SetEndpoint(name string, getter TypeGetter) {
	c.endpointTypes[name] = getter
}

// ForeachEndpointType calls cb on every endpoint type registered with the Config
func (c Config) ForeachEndpointType(cb func(string, TypeGetter) error) error {
	for n, ep := range c.endpointTypes {
		if err := cb(n, ep); err != nil {
			return err
		}
	}
	return nil
}

// NewConfig creates a config object
func NewConfig(contextType TypeGetter, endpoints ...NamedTypeGetter) Config {
	res := Config{
		contextType:   contextType,
		endpointTypes: make(map[string]TypeGetter),
	}
	for _, e := range endpoints {
		res.endpointTypes[e.name] = e.typeGetter
	}
	return res
}
