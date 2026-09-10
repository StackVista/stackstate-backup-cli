FROM registry.suse.com/bci/bci-micro:15.7@sha256:9e01097b36048042e276dd40e7661941ac4ba909237904bae99540eb90c9a5c6
COPY sts-backup /usr/local/bin/sts-backup
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/sts-backup"]
