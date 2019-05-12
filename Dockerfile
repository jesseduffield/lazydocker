# run with:
# docker build -t lazydocker .
# docker run -it lazydocker:latest /bin/sh -l

FROM golang:alpine
WORKDIR /go/src/github.com/jesseduffield/lazydocker/
COPY ./ .
RUN CGO_ENABLED=0 GOOS=linux go build

FROM alpine:latest
RUN apk add -U git xdg-utils
WORKDIR /go/src/github.com/jesseduffield/lazydocker/
COPY --from=0 /go/src/github.com/jesseduffield/lazydocker /go/src/github.com/jesseduffield/lazydocker
COPY --from=0 /go/src/github.com/jesseduffield/lazydocker/lazydocker /bin/
RUN echo "alias gg=lazydocker" >> ~/.profile
