variable "IMAGE" {
  default = "ipv6-proxy:local"
}

variable "GO_VERSION" {
  default = "1.25"
}

group "default" {
  targets = ["local-amd64"]
}

target "image" {
  context    = "."
  dockerfile = "Dockerfile"
  args = {
    GO_VERSION = GO_VERSION
  }
  tags       = [IMAGE]
  platforms  = ["linux/amd64", "linux/arm64"]
}

# docker buildx bake local-amd64
target "local-amd64" {
  inherits   = ["image"]
  platforms  = ["linux/amd64"]
  output     = ["type=docker"]
}

# docker buildx bake local-arm64
target "local-arm64" {
  inherits   = ["image"]
  platforms  = ["linux/arm64"]
  output     = ["type=docker"]
}
