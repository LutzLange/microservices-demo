FROM microsoft/dotnet:2.1-sdk as builder
WORKDIR /app
COPY . .
RUN dotnet add package Instana.Profiler.Linux.Bundled.Refs --version 1.0.54.5 && \
    dotnet restore && \
    dotnet build && \
    dotnet publish -c release -r linux-x64 -o /cartservice

# cartservice
FROM debian:stretch-slim

# Dependencies for runtime
RUN apt-get update -y && apt-get install -y \
    wget \
	libc++1 \
	libgcc1 \
	libstdc++6 && \
	ln -s /usr/lib/x86_64-linux-musl/libc.so /lib/libc.musl-x86_64.so.1

RUN GRPC_HEALTH_PROBE_VERSION=v0.2.0 && \
    wget -qO/bin/grpc_health_probe https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64 && \
    chmod +x /bin/grpc_health_probe
WORKDIR /app
COPY --from=builder /cartservice .
ENTRYPOINT ["./cartservice", "start"]
