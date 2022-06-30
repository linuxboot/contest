This example provides a tiny HTTP server which has metrics and is able to export them. An example session is below

---

Let's get the metrics:
```
$ curl http://localhost:8080/metrics
$
```
No metrics. Makes sense, we haven't triggered anything that makes any metrics. Let's trigger something and try again:
```
$ curl http://localhost:8080/
Hello!
```
```
$ curl http://localhost:8080/metrics
# HELP concurrent_requests_int
# TYPE concurrent_requests_int gauge
concurrent_requests_int 0
# HELP hello_requests_count
# TYPE hello_requests_count counter
hello_requests_count 1
# HELP response_count
# TYPE response_count counter
response_count{status="200"} 1
$
```
OK, now we see metrics. Let's add some more:
```
$ curl http://localhost:8080/sleep?secs=2.5
done
$
```
```
$ curl http://localhost:8080/sleep?secs=0.1
done
$
```
$ curl http://localhost:8080/metrics
# HELP concurrent_requests_int
# TYPE concurrent_requests_int gauge
concurrent_requests_int 0
# HELP hello_requests_count
# TYPE hello_requests_count counter
hello_requests_count 1
# HELP response_count
# TYPE response_count counter
response_count{status="200"} 3
# HELP slept_ns_count
# TYPE slept_ns_count counter
slept_ns_count 2.6e+09
$
```
As we can see `response_count{status="200"}` is `3` and `slept_ns_count` is 2.6 seconds. Everything is correct.
