#TEEBIRD

Tee for TCP traffic

	./teebird -l <listen address> -r <responder address> -f <forward address>

##Options

- `listen address` is the address that `teebird` will listen for incoming requests.
- `responder address` is the endpoint that is used to answer incoming requests.
- `forward address` is the address to which TCP traffic is forwarded to.

##Example

Setup netcat to listen to `localhost:8082`

	nc -lk 8082

Start your tomcat service on `localhost:8080`

	service tomcat start

Start `teebird` on `localhost:8081`

	./teebird -l :8081 -r :8080 -f :8082
