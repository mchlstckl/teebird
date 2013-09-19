#TEEBIRD

Tee for TCP traffic

	./teebird -l <listen address> \
		-r <responder address> \
		-f <forward address> \
		-x=<responder uses RN as stop> \
		-w=<write files>

##Options

Just type `./teebird --help` to display options.

- `listen address` is the address that `teebird` will listen for incoming requests, default is localhost:8081.
- `responder address` is the endpoint that is used to answer incoming requests, default is localhost:8080.
- `forward address` is the address to which TCP traffic is forwarded to, default is localhost:8082.
- `responder uses RN as stop` indicates that \r\n means stop, default is true.
- `write files` write to file option, default is true. Will create files responder.log, forwarder.log and listener.log.

##Example

Setup netcat to listen to `localhost:8082`

	nc -lk 8082

Start your service (e.g. tomcat) on `localhost:8080`

	service tomcat start

Start `teebird` on `localhost:8081`

	./teebird -l :8081 -r :8080 -f :8082

##Note

`python -m SimpleHTTPServer` requires that `-x=false`.
