# Lite-engine

How to use:

* Create an .env file. It can be empty.
* To build linux and windows `GOOS=windows go build -o lite-engine.exe; go build`
* Generate tls credentials: go run main.go certs
* Start server: go run main.go server
* Client call to check health status of server: go run main.go client.
