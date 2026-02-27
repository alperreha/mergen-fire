service {
  name = "mergen-xds-center"
  id   = "mergen-xds-center"
  port = 18080
  tags = ["control-plane", "xds-starter"]

  check {
    name     = "xds-center-health"
    http     = "http://127.0.0.1:18080/healthz"
    method   = "GET"
    interval = "10s"
    timeout  = "2s"
  }
}
