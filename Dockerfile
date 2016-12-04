FROM ubuntu:xenial
MAINTAINER Simon Gomizelj <sgomizelj@sangoma.com>

EXPOSE 5900
RUN apt-get -y update && apt-get -y upgrade && apt-get -y install qemu-kvm iproute2 iptables

WORKDIR /gerty
ADD entrypoint /usr/bin/entrypoint
ADD gerty /usr/bin/gerty
ENTRYPOINT ["/usr/bin/entrypoint"]
