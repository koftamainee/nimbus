fn main() {
    tonic_build::configure()
        .build_client(true)
        .build_server(false)
        .compile_protos(
            &["../proto/quorum/v1/kv.proto", "../proto/quorum/v1/watch.proto"],
            &["../proto"],
        )
        .unwrap();
}
