FROM golang:1.14-alpine

RUN apk --no-cache add py3-pip make git zip

RUN pip3 install cloudformation-cli-go-plugin

COPY . /build

WORKDIR /build

RUN go mod download

RUN make -f Makefile.package package

CMD mkdir -p /output/ && mv /build/awsqs-kubernetes-helm.zip /output/
