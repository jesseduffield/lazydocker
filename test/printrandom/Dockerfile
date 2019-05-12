# building the binary
FROM golang:1.11 as builder

# Add Maintainer Info
LABEL maintainer="Jesse Duffield <jessedduffield@gmail.com>"

# Set the Current Working Directory inside the container
WORKDIR /src/github.com/jesseduffield/lazydocker/test/printrandom

# Copy everything from the current directory to the PWD(Present Working Directory) inside the container
COPY . .

# Build the package
RUN go build

# putting binary into a minimal image
FROM scratch

WORKDIR /root/

COPY --from=builder /src/github.com/jesseduffield/lazydocker/test/printrandom/printrandom .

# Run the executable
CMD ["./printrandom"]