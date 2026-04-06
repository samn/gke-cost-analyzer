FROM gcr.io/distroless/static-debian12:nonroot
COPY dist/gke-cost-analyzer-linux-amd64 /usr/local/bin/gke-cost-analyzer
RUN ["gke-cost-analyzer", "--help"]
ENTRYPOINT ["gke-cost-analyzer"]
