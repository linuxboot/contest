FROM golang:1.18-buster

RUN apt-get update && apt-get install -y mariadb-client openssh-server

# setup sshd for some plugin tests
RUN ssh-keygen -q -t rsa -f /root/.ssh/id_rsa -N ""
RUN cat /root/.ssh/id_rsa.pub > /root/.ssh/authorized_keys

WORKDIR ${GOPATH}/src/github.com/linuxboot/contest
COPY  . .
RUN go get -t -v ./...
RUN chmod a+x docker/contest/tests.sh
