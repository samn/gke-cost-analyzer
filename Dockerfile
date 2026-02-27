FROM alpine:3 AS fetch
ARG VERSION=latest
RUN apk add --no-cache curl jq \
 && if [ "$VERSION" = "latest" ]; then \
      VERSION=$(curl -fsSL https://api.github.com/repos/samn/autopilot-cost-analyzer/releases/latest | jq -r .tag_name); \
    fi \
 && curl -fsSL -o /autopilot-cost-analyzer \
      "https://github.com/samn/autopilot-cost-analyzer/releases/download/${VERSION}/autopilot-cost-analyzer-linux-amd64" \
 && chmod +x /autopilot-cost-analyzer

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=fetch /autopilot-cost-analyzer /usr/local/bin/autopilot-cost-analyzer
ENTRYPOINT ["autopilot-cost-analyzer"]
