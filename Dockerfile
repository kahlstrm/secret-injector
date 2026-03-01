FROM scratch

COPY secret-injector /secret-injector

ENTRYPOINT ["/secret-injector"]
