package main

import (
	"github.com/anacrolix/dms/server"
)


func main() {
	server.Start(lightDevice)
}


// TODO: 
// The control URL for every service is the same. We're able to infer the desired service from the request headers.
func init() {
	for _, s := range services {
		s.ControlURL = serviceControlURL
	}
}