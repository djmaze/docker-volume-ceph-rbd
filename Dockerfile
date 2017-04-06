FROM alpine

RUN mkdir -p /run/docker/plugins /mnt/state /mnt/volumes

COPY docker-volume-ceph-rbd docker-volume-ceph-rbd

CMD ["docker-volume-ceph-rbd"]
