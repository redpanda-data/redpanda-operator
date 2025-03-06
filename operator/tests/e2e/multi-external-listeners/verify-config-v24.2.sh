#!/usr/bin/env bash

expected=$(
  cat <<EOF
redpanda:
    data_directory: /var/lib/redpanda/data
    empty_seed_starts_cluster: false
    seed_servers:
        - host:
            address: multi-external-listeners-0.multi-external-listeners.${NAMESPACE}.svc.cluster.local.
            port: 33145
    rpc_server:
        address: 0.0.0.0
        port: 33145
    kafka_api:
        - address: 0.0.0.0
          port: 9092
          name: kafka
          authentication_method: sasl
        - address: 0.0.0.0
          port: 30092
          name: kafka-external
          authentication_method: mtls_identity
        - address: 0.0.0.0
          port: 30094
          name: kafka-sasl
          authentication_method: sasl
    kafka_api_tls:
        - name: kafka-external
          key_file: /etc/tls/certs/tls.key
          cert_file: /etc/tls/certs/tls.crt
          truststore_file: /etc/tls/certs/ca/ca.crt
          enabled: true
          require_client_auth: true
        - name: kafka-sasl
          key_file: /etc/tls/certs/tls.key
          cert_file: /etc/tls/certs/tls.crt
          enabled: true
    admin:
        - address: 0.0.0.0
          port: 9644
          name: admin
    advertised_rpc_api:
        address: multi-external-listeners-0.multi-external-listeners.${NAMESPACE}.svc.cluster.local.
        port: 33145
    advertised_kafka_api:
        - address: multi-external-listeners-0.multi-external-listeners.${NAMESPACE}.svc.cluster.local.
          port: 9092
          name: kafka
        - address: test-0.redpanda.com
          port: 30092
          name: kafka-external
        - address: test-0.redpanda.com
          port: 30094
          name: kafka-sasl
    developer_mode: true
    auto_create_topics_enabled: true
    cloud_storage_segment_max_upload_interval_sec: 1800
    enable_rack_awareness: true
    fetch_reads_debounce_timeout: 10
    group_initial_rebalance_delay: 0
    group_topic_partitions: 3
    log_segment_size: 536870912
    log_segment_size_min: 1
    storage_min_free_bytes: 10485760
    topic_partitions_per_shard: 1000
    write_caching_default: "true"
rpk:
    overprovisioned: true
    tune_network: true
    tune_disk_scheduler: true
    tune_disk_nomerges: true
    tune_disk_write_cache: true
    tune_disk_irq: true
    tune_cpu: true
    tune_aio_events: true
    tune_clocksource: true
    tune_swappiness: true
    coredump_dir: /var/lib/redpanda/coredump
    tune_ballast_file: true
pandaproxy:
    pandaproxy_api:
        - address: 0.0.0.0
          port: 8082
          name: proxy
        - address: 0.0.0.0
          port: 30084
          name: proxy-sasl-npe
          authentication_method: http_basic
        - address: 0.0.0.0
          port: 30086
          name: proxy-sasl
          authentication_method: http_basic
        - address: 0.0.0.0
          port: 30082
          name: proxy-mtls
          authentication_method: http_basic
    pandaproxy_api_tls:
        - name: proxy-sasl-npe
          key_file: /etc/tls/certs/pandaproxy/tls.key
          cert_file: /etc/tls/certs/pandaproxy/tls.crt
          enabled: true
        - name: proxy-sasl
          key_file: /etc/tls/certs/pandaproxy/tls.key
          cert_file: /etc/tls/certs/pandaproxy/tls.crt
          enabled: true
        - name: proxy-mtls
          key_file: /etc/tls/certs/pandaproxy/tls.key
          cert_file: /etc/tls/certs/pandaproxy/tls.crt
          truststore_file: /etc/tls/certs/pandaproxy/ca/ca.crt
          enabled: true
          require_client_auth: true
    advertised_pandaproxy_api:
        - address: multi-external-listeners-0.multi-external-listeners.${NAMESPACE}.svc.cluster.local.
          port: 8082
          name: proxy
        - address: test-0.redpanda.com
          port: 30084
          name: proxy-sasl-npe
        - address: test-0.redpanda.com
          port: 30086
          name: proxy-sasl
        - address: test-0.redpanda.com
          port: 30082
          name: proxy-mtls
pandaproxy_client:
    brokers:
        - address: multi-external-listeners.${NAMESPACE}.svc.cluster.local.
          port: 9092
schema_registry:
    schema_registry_api:
        - address: 0.0.0.0
          port: 8081
          name: schema-registry
        - address: 0.0.0.0
          port: 30083
          name: sr-sasl
          authentication_method: http_basic
    schema_registry_api_tls:
        - name: schema-registry
          key_file: /etc/tls/certs/schema-registry/tls.key
          cert_file: /etc/tls/certs/schema-registry/tls.crt
          enabled: true
        - name: sr-sasl
          key_file: /etc/tls/certs/schema-registry/tls.key
          cert_file: /etc/tls/certs/schema-registry/tls.crt
          truststore_file: /etc/tls/certs/schema-registry/ca/ca.crt
          enabled: true
          require_client_auth: true
schema_registry_client:
    brokers:
        - address: multi-external-listeners.${NAMESPACE}.svc.cluster.local.
          port: 9092
EOF
)
