
A simple terminal UI for Podman and podman-compose, written in Go with the [gocui](https://github.com/jroimartin/gocui 'gocui') library.

> **Fork Notice:** This project is a fork of [lazydocker](https://github.com/jesseduffield/lazydocker) by Jesse Duffield, converted to use Podman's native libpod library instead of the Docker SDK.

[![Go Report Card](https://goreportcard.com/badge/github.com/christophe-duc/lazypodman)](https://goreportcard.com/report/github.com/christophe-duc/lazypodman)
[![GolangCI](https://golangci.com/badges/github.com/christophe-duc/lazypodman.svg)](https://golangci.com)
[![GoDoc](https://godoc.org/github.com/christophe-duc/lazypodman?status.svg)](http://godoc.org/github.com/christophe-duc/lazypodman)
![GitHub repo size](https://img.shields.io/github/repo-size/christophe-duc/lazypodman)
[![GitHub Releases](https://img.shields.io/github/downloads/christophe-duc/lazypodman/total)](https://github.com/christophe-duc/lazypodman/releases)
[![GitHub tag](https://img.shields.io/github/tag/christophe-duc/lazypodman.svg)](https://github.com/christophe-duc/lazypodman/releases/latest)
[![homebrew](https://img.shields.io/homebrew/v/lazypodman)](https://github.com/Homebrew/homebrew-core/blob/master/Formula/lazypodman.rb)

![Gif](/docs/resources/demo3.gif)

[Demo](https://youtu.be/NICqQPxwJWw)

## Elevator Pitch

Again, this is a fork! and probably with reduced functionality as the original from Jesse Duffield. It was created to resolve a simple problem, work fully with podman and don't depend on how docker works and the socket.

Lazypodman DOES support pods

This is published as is. Compilation works and lazypodman runs on Linux without needing a socket present to monitor your containers.

Original elevator pitch below:

Minor rant incoming: Something's not working? Maybe a service is down. `podman-compose ps`. Yep, it's that microservice that's still buggy. No issue, I'll just restart it: `podman-compose restart`. Okay now let's try again. Oh wait the issue is still there. Hmm. `podman-compose ps`. Right so the service must have just stopped immediately after starting. I probably would have known that if I was reading the log stream, but there is a lot of clutter in there from other services. I could get the logs for just that one service with `podman-compose logs --follow myservice` but that dies everytime the service dies so I'd need to run that command every time I restart the service. I could alternatively run `podman-compose up myservice` and in that terminal window if the service is down I could just `up` it again, but now I've got one service hogging a terminal window even after I no longer care about its logs. I guess when I want to reclaim the terminal realestate I can do `ctrl+P,Q`, but... wait, that's not working for some reason. Should I use ctrl+C instead? I can't remember if that closes the foreground process or kills the actual service.

What a headache!

Memorising podman commands is hard. Memorising aliases is slightly less hard. Keeping track of your containers across multiple terminal windows is near impossible. What if you had all the information you needed in one terminal window with every common command living one keypress away (and the ability to add custom commands as well). Lazypodman's goal is to make that dream a reality.

- [Requirements](https://github.com/christophe-duc/lazypodman#requirements)
- [Installation](https://github.com/christophe-duc/lazypodman#installation)
- [Usage](https://github.com/christophe-duc/lazypodman#usage)
- [Keybindings](/docs/keybindings)
- [Cool Features](https://github.com/christophe-duc/lazypodman#cool-features)
- [Contributing](https://github.com/christophe-duc/lazypodman#contributing)
- [Video Tutorial](https://youtu.be/NICqQPxwJWw)
- [Config Docs](/docs/Config.md)
- [FAQ](https://github.com/christophe-duc/lazypodman#faq)

## Requirements

- Podman >= **5.0**
- podman-compose >= **1.0** (optional, for compose support)

## Installation

### Binary Release (Linux)

You can manually download a binary release from [the release page](https://github.com/christophe-duc/lazypodman/releases).

Automated install/update, don't forget to always verify what you're piping into bash:

```sh
curl https://raw.githubusercontent.com/christophe-duc/lazypodman/master/scripts/install_update_linux.sh | bash
```
The script installs downloaded binary to `$HOME/.local/bin` directory by default, but it can be changed by setting `DIR` environment variable.

### Go

Required Go Version >= **1.19**

```sh
go install github.com/christophe-duc/lazypodman@latest
```

Required Go version >= **1.8**, <= **1.17**

```sh
go get github.com/christophe-duc/lazypodman
```

### Podman Container

1. <details><summary>Click if you have an ARM device</summary><p>

    - If you have a ARM 32 bit v6 architecture

        ```sh
        podman build -t lazypodman \
        --build-arg BASE_IMAGE_BUILDER=arm32v6/golang \
        --build-arg GOARCH=arm \
        --build-arg GOARM=6 \
        https://github.com/christophe-duc/lazypodman.git
        ```

    - If you have a ARM 32 bit v7 architecture

        ```sh
        podman build -t lazypodman \
        --build-arg BASE_IMAGE_BUILDER=arm32v7/golang \
        --build-arg GOARCH=arm \
        --build-arg GOARM=7 \
        https://github.com/christophe-duc/lazypodman.git
        ```

    - If you have a ARM 64 bit v8 architecture

        ```sh
        podman build -t lazypodman \
        --build-arg BASE_IMAGE_BUILDER=arm64v8/golang \
        --build-arg GOARCH=arm64 \
        https://github.com/christophe-duc/lazypodman.git
        ```

    </p></details>

1. Run the container

    ```sh
    # Rootful Podman
    podman run --rm -it \
    -v /run/podman/podman.sock:/run/podman/podman.sock:ro \
    -v /yourpath:/.config/lazypodman \
    ghcr.io/christophe-duc/lazypodman

    # Rootless Podman
    podman run --rm -it \
    -v $XDG_RUNTIME_DIR/podman/podman.sock:/run/podman/podman.sock:ro \
    -v /yourpath:/.config/lazypodman \
    ghcr.io/christophe-duc/lazypodman
    ```

    - Don't forget to change `/yourpath` to an actual path you created to store lazypodman's config
    - You can also use this [podman-compose.yml](https://github.com/christophe-duc/lazypodman/blob/master/podman-compose.yml)
    - You might want to create an alias, for example:

        ```sh
        echo "alias lzd='podman run --rm -it -v \$XDG_RUNTIME_DIR/podman/podman.sock:/run/podman/podman.sock:ro -v /yourpath/config:/.config/lazypodman ghcr.io/christophe-duc/lazypodman'" >> ~/.zshrc
        ```



For development, you can build the image using:

```sh
git clone https://github.com/christophe-duc/lazypodman.git
cd lazypodman
podman build -t lazypodman \
    --build-arg BUILD_DATE=`date -u +"%Y-%m-%dT%H:%M:%SZ"` \
    --build-arg VCS_REF=`git rev-parse --short HEAD` \
    --build-arg VERSION=`git describe --abbrev=0 --tag` \
    .
```

### Manual

You'll need to [install Go](https://golang.org/doc/install)

```
git clone https://github.com/christophe-duc/lazypodman.git
cd lazypodman
go install
```

You can also use `go run main.go` to compile and run in one go (pun definitely intended)

## Usage

Call `lazypodman` in your terminal. I personally use this a lot so I've made an alias for it like so:

```
echo "alias lzd='lazypodman'" >> ~/.zshrc
```

(you can substitute .zshrc for whatever rc file you're using)

- Basic video tutorial [here](https://youtu.be/NICqQPxwJWw).
- List of keybindings
  [here](/docs/keybindings).

## Cool features

everything is one keypress away (or one click away! Mouse support FTW):

- viewing the state of your Podman or podman-compose container environment at a glance
- viewing logs for a container/service
- viewing ascii graphs of your containers' metrics so that you can not only feel but also look like a developer
- customising those graphs to measure nearly any metric you want
- attaching to a container/service
- restarting/removing/rebuilding containers/services
- viewing the ancestor layers of a given image
- pruning containers, images, or volumes that are hogging up disk space

## Contributing

There is still a lot of work to go! Please check out the [contributing guide](CONTRIBUTING.md).


## FAQ

### How do I edit my config?

By opening lazypodman, clicking on the 'project' panel in the top left, and pressing 'o' (or 'e' if your editor is vim). See [Config Docs](/docs/Config.md)

### How do I get text to wrap in my main panel?

In the future I want to make this the default, but for now there are some CPU issues that arise with wrapping. If you want to enable wrapping, use `gui.wrapMainPanel: true`

### How do you select text?

Because we support mouse events, you will need to hold option while dragging the mouse to indicate you're trying to select text rather than click on something. Alternatively you can disable mouse events via the `gui.ignoreMouseEvents` config value.

Mac Users: See [Issue #190](https://github.com/christophe-duc/lazypodman/issues/190) for other options.

### Why can't I see my container's logs?

By default we only show logs from the last hour, so that we're not putting too much strain on the machine. This may be why you can't see logs when you first start lazypodman. This can be overwritten in the config's `commandTemplates`

If you are running lazypodman in a Podman container, it is a known bug that you can't see logs or CPU usage.

## Alternatives

- [lazydocker](https://github.com/jesseduffield/lazydocker) - The original project this was forked from, for Docker users.
- [podman-tui](https://github.com/containers/podman-tui) - Another terminal UI for Podman from the Containers project.
- [Portainer](https://github.com/portainer/portainer) - Portainer tries to solve the same problem but it's accessed via your browser rather than your terminal.
- See [Awesome Podman list](https://github.com/containers/awesome-podman) for similar tools to work with Podman.
