// Package config holds the settings of the server and the environment that is built from them.
//
// The command line sets one [Options]. [Options.Validate] reports every rule that the settings break, so that the
// server can refuse to start. [NewEnv] then builds every concrete dependency of the server, once, into an [Env]: the
// engine, the TURN issuer, the AI presenter, the runtime monitor and the page. The HTTP server is built from the Env
// by api.NewServer. This is the only package that knows every concrete type and puts them together.
package config
