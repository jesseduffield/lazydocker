<p align="center">
  <img src="https://user-images.githubusercontent.com/8456633/59972109-8e9c8480-95cc-11e9-8350-38f7f86ba76d.png">
</p>

A simple terminal UI for both docker and docker-compose, written in Go with the [gocui](https://github.com/jroimartin/gocui 'gocui') library.

![CI](https://github.com/jesseduffield/lazygit/workflows/Continuous%20Integration/badge.svg)
[![Go Report Card](https://goreportcard.com/badge/github.com/jesseduffield/lazydocker)](https://goreportcard.com/report/github.com/jesseduffield/lazydocker)
[![GolangCI](https://golangci.com/badges/github.com/jesseduffield/lazydocker.svg)](https://golangci.com)
[![GoDoc](https://godoc.org/github.com/jesseduffield/lazydocker?status.svg)](http://godoc.org/github.com/jesseduffield/lazydocker)
![GitHub repo size](https://img.shields.io/github/repo-size/jesseduffield/lazydocker)
[![GitHub Releases](https://img.shields.io/github/downloads/jesseduffield/lazydocker/total)](https://github.com/jesseduffield/lazydocker/releases)
[![GitHub tag](https://img.shields.io/github/tag/jesseduffield/lazydocker.svg)](https://github.com/jesseduffield/lazydocker/releases/latest)
[![homebrew](https://img.shields.io/homebrew/v/lazydocker)](https://github.com/Homebrew/homebrew-core/blob/master/Formula/lazydocker.rb)

![Gif](/docs/resources/demo3.gif)

[Demo](https://youtu.be/NICqQPxwJWw)

## Sponsors

<p align="center">
 Maintainence of this project is made possible by all the <a href="https://github.com/jesseduffield/lazydocker/graphs/contributors">contributors</a> and <a href="https://github.com/sponsors/jesseduffield">sponsors</a>. If you'd like to sponsor this project and have your avatar or company logo appear below <a href="https://github.com/sponsors/jesseduffield">click here</a>. 💙
</p>

<p align="center">
<!-- sponsors --><a href="https://github.com/atecce"><img src="https://github.com/atecce.png" width="60px" alt="" /></a><a href="https://github.com/intabulas"><img src="https://github.com/intabulas.png" width="60px" alt="" /></a><a href="https://github.com/peppy"><img src="https://github.com/peppy.png" width="60px" alt="" /></a><a href="https://github.com/piot"><img src="https://github.com/piot.png" width="60px" alt="" /></a><a href="https://github.com/kristijanhusak"><img src="https://github.com/kristijanhusak.png" width="60px" alt="" /></a><a href="https://github.com/blacky14"><img src="https://github.com/blacky14.png" width="60px" alt="" /></a><a href="https://github.com/rgwood"><img src="https://github.com/rgwood.png" width="60px" alt="" /></a><a href="https://github.com/oliverguenther"><img src="https://github.com/oliverguenther.png" width="60px" alt="" /></a><a href="https://github.com/pawanjay176"><img src="https://github.com/pawanjay176.png" width="60px" alt="" /></a><a href="https://github.com/bdach"><img src="https://github.com/bdach.png" width="60px" alt="" /></a><a href="https://github.com/naoey"><img src="https://github.com/naoey.png" width="60px" alt="" /></a><a href="https://github.com/jryom"><img src="https://github.com/jryom.png" width="60px" alt="" /></a><a href="https://github.com/sagor999"><img src="https://github.com/sagor999.png" width="60px" alt="" /></a><a href="https://github.com/fargozhu"><img src="https://github.com/fargozhu.png" width="60px" alt="" /></a><a href="https://github.com/carstengehling"><img src="https://github.com/carstengehling.png" width="60px" alt="" /></a><a href="https://github.com/ceuk"><img src="https://github.com/ceuk.png" width="60px" alt="" /></a><a href="https://github.com/akospwc"><img src="https://github.com/akospwc.png" width="60px" alt="" /></a><a href="https://github.com/peterdieleman"><img src="https://github.com/peterdieleman.png" width="60px" alt="" /></a><a href="https://github.com/Xetera"><img src="https://github.com/Xetera.png" width="60px" alt="" /></a><a href="https://github.com/HoldenLucas"><img src="https://github.com/HoldenLucas.png" width="60px" alt="" /></a><a href="https://github.com/barbados-clemens"><img src="https://github.com/barbados-clemens.png" width="60px" alt="" /></a><a href="https://github.com/nartc"><img src="https://github.com/nartc.png" width="60px" alt="" /></a><a href="https://github.com/"><img src="https://github.com/.png" width="60px" alt="" /></a><a href="https://github.com/matejcik"><img src="https://github.com/matejcik.png" width="60px" alt="" /></a><a href="https://github.com/lucatume"><img src="https://github.com/lucatume.png" width="60px" alt="" /></a><a href="https://github.com/dbabiak"><img src="https://github.com/dbabiak.png" width="60px" alt="" /></a><a href="https://github.com/davidlattimore"><img src="https://github.com/davidlattimore.png" width="60px" alt="" /></a><a href="https://github.com/zach-fuller"><img src="https://github.com/zach-fuller.png" width="60px" alt="" /></a><a href="https://github.com/escrafford"><img src="https://github.com/escrafford.png" width="60px" alt="" /></a><a href="https://github.com/davdroman"><img src="https://github.com/davdroman.png" width="60px" alt="" /></a><a href="https://github.com/KowalskiPiotr98"><img src="https://github.com/KowalskiPiotr98.png" width="60px" alt="" /></a><a href="https://github.com/IvanZuy"><img src="https://github.com/IvanZuy.png" width="60px" alt="" /></a><a href="https://github.com/nicholascloud"><img src="https://github.com/nicholascloud.png" width="60px" alt="" /></a><a href="https://github.com/topher200"><img src="https://github.com/topher200.png" width="60px" alt="" /></a><a href="https://github.com/"><img src="https://github.com/.png" width="60px" alt="" /></a><a href="https://github.com/PhotonQuantum"><img src="https://github.com/PhotonQuantum.png" width="60px" alt="" /></a><a href="https://github.com/GitSquared"><img src="https://github.com/GitSquared.png" width="60px" alt="" /></a><a href="https://github.com/ava1ar"><img src="https://github.com/ava1ar.png" width="60px" alt="" /></a><a href="https://github.com/alqh"><img src="https://github.com/alqh.png" width="60px" alt="" /></a><a href="https://github.com/pedropombeiro"><img src="https://github.com/pedropombeiro.png" width="60px" alt="" /></a><a href="https://github.com/minidfx"><img src="https://github.com/minidfx.png" width="60px" alt="" /></a><a href="https://github.com/JoeKlemmer"><img src="https://github.com/JoeKlemmer.png" width="60px" alt="" /></a><a href="https://github.com/MikaelElkiaer"><img src="https://github.com/MikaelElkiaer.png" width="60px" alt="" /></a><a href="https://github.com/smoogipoo"><img src="https://github.com/smoogipoo.png" width="60px" alt="" /></a><a href="https://github.com/Mo8it"><img src="https://github.com/Mo8it.png" width="60px" alt="" /></a><a href="https://github.com/marcusyqy"><img src="https://github.com/marcusyqy.png" width="60px" alt="" /></a><a href="https://github.com/jfavery"><img src="https://github.com/jfavery.png" width="60px" alt="" /></a><a href="https://github.com/V1RE"><img src="https://github.com/V1RE.png" width="60px" alt="" /></a><a href="https://github.com/ShivamJoker"><img src="https://github.com/ShivamJoker.png" width="60px" alt="" /></a><a href="https://github.com/sikos"><img src="https://github.com/sikos.png" width="60px" alt="" /></a><a href="https://github.com/SimonBaars"><img src="https://github.com/SimonBaars.png" width="60px" alt="" /></a><a href="https://github.com/JensPfeifle"><img src="https://github.com/JensPfeifle.png" width="60px" alt="" /></a><a href="https://github.com/Yuuki77"><img src="https://github.com/Yuuki77.png" width="60px" alt="" /></a><a href="https://github.com/superDuperCyberTechno"><img src="https://github.com/superDuperCyberTechno.png" width="60px" alt="" /></a><a href="https://github.com/ColonelBucket8"><img src="https://github.com/ColonelBucket8.png" width="60px" alt="" /></a><a href="https://github.com/mmachatschek"><img src="https://github.com/mmachatschek.png" width="60px" alt="" /></a><a href="https://github.com/mutewinter"><img src="https://github.com/mutewinter.png" width="60px" alt="" /></a><a href="https://github.com/schneipp"><img src="https://github.com/schneipp.png" width="60px" alt="" /></a><!-- sponsors -->
</p>

## Elevator Pitch

Minor rant incoming: Something's not working? Maybe a service is down. `docker-compose ps`. Yep, it's that microservice that's still buggy. No issue, I'll just restart it: `docker-compose restart`. Okay now let's try again. Oh wait the issue is still there. Hmm. `docker-compose ps`. Right so the service must have just stopped immediately after starting. I probably would have known that if I was reading the log stream, but there is a lot of clutter in there from other services. I could get the logs for just that one service with `docker compose logs --follow myservice` but that dies everytime the service dies so I'd need to run that command every time I restart the service. I could alternatively run `docker-compose up myservice` and in that terminal window if the service is down I could just `up` it again, but now I've got one service hogging a terminal window even after I no longer care about its logs. I guess when I want to reclaim the terminal realestate I can do `ctrl+P,Q`, but... wait, that's not working for some reason. Should I use ctrl+C instead? I can't remember if that closes the foreground process or kills the actual service.

What a headache!

Memorising docker commands is hard. Memorising aliases is slightly less hard. Keeping track of your containers across multiple terminal windows is near impossible. What if you had all the information you needed in one terminal window with every common command living one keypress away (and the ability to add custom commands as well). Lazydocker's goal is to make that dream a reality.

- [Requirements](https://github.com/jesseduffield/lazydocker#requirements)
- [Installation](https://github.com/jesseduffield/lazydocker#installation)
- [Usage](https://github.com/jesseduffield/lazydocker#usage)
- [Keybindings](/docs/keybindings)
- [Cool Features](https://github.com/jesseduffield/lazydocker#cool-features)
- [Contributing](https://github.com/jesseduffield/lazydocker#contributing)
- [Video Tutorial](https://youtu.be/NICqQPxwJWw)
- [Config Docs](/docs/Config.md)
- [Twitch Stream](https://www.twitch.tv/jesseduffield)
- [FAQ](https://github.com/jesseduffield/lazydocker#faq)

## Requirements

- Docker >= **1.13** (API >= **1.25**)
- Docker-Compose >= **1.23.2** (optional)

## Installation

### Homebrew

Normally `lazydocker` formula can be found in the Homebrew core but we suggest you to tap our formula to get frequently updated one. It works with Linux, too.

**Tap**:
```sh
brew install jesseduffield/lazydocker/lazydocker
```

**Core**:
```sh
brew install lazydocker
```

### Scoop (Windows)

You can install `lazydocker` using [scoop](https://scoop.sh/):

```sh
scoop install lazydocker
```
### Chocolatey (Windows)

You can install `lazydocker` using [Chocolatey](https://chocolatey.org/):

```sh
choco install lazydocker
```
### asdf-vm

You can install [asdf-lazydocker plugin](https://github.com/comdotlinux/asdf-lazydocker) using [asdf-vm](https://asdf-vm.com/):
#### Setup (Once)
```sh
asdf plugin add lazydocker https://github.com/comdotlinux/asdf-lazydocker.git
```

#### For Install / Upgrade
```sh
asdf list all lazydocker
asdf install lazydocker 0.12
asdf global lazydocker 0.12
```

### Binary Release (Linux/OSX/Windows)

You can manually download a binary release from [the release page](https://github.com/jesseduffield/lazydocker/releases).

Automated install/update, don't forget to always verify what you're piping into bash:

```sh
curl https://raw.githubusercontent.com/jesseduffield/lazydocker/master/scripts/install_update_linux.sh | bash
```
The script installs downloaded binary to `$HOME/.local/bin` directory by default, but it can be changed by setting `DIR` environment variable.

### Go

Required Go Version >= **1.16**

```sh
go install github.com/jesseduffield/lazydocker@latest
```

Required Go version >= **1.8**, <= **1.17**

```sh
go get github.com/jesseduffield/lazydocker
```

### Arch Linux AUR

You can install lazydocker using the [AUR](https://aur.archlinux.org/packages/lazydocker) by running:

```sh
yay -S lazydocker
```

### Docker

[![Docker Pulls](https://img.shields.io/docker/pulls/lazyteam/lazydocker.svg)](https://hub.docker.com/r/lazyteam/lazydocker)
[![Docker Stars](https://img.shields.io/docker/stars/lazyteam/lazydocker.svg)](https://hub.docker.com/r/lazyteam/lazydocker)
[![Docker Automated](https://img.shields.io/docker/cloud/automated/lazyteam/lazydocker.svg)](https://hub.docker.com/r/lazyteam/lazydocker)

1. <details><summary>Click if you have an ARM device</summary><p>

    - If you have a ARM 32 bit v6 architecture

        ```sh
        docker build -t lazyteam/lazydocker \
        --build-arg BASE_IMAGE_BUILDER=arm32v6/golang \
        --build-arg GOARCH=arm \
        --build-arg GOARM=6 \
        https://github.com/jesseduffield/lazydocker.git
        ```

    - If you have a ARM 32 bit v7 architecture

        ```sh
        docker build -t lazyteam/lazydocker \
        --build-arg BASE_IMAGE_BUILDER=arm32v7/golang \
        --build-arg GOARCH=arm \
        --build-arg GOARM=7 \
        https://github.com/jesseduffield/lazydocker.git
        ```

    - If you have a ARM 64 bit v8 architecture

        ```sh
        docker build -t lazyteam/lazydocker \
        --build-arg BASE_IMAGE_BUILDER=arm64v8/golang \
        --build-arg GOARCH=arm64 \
        https://github.com/jesseduffield/lazydocker.git
        ```

    </p></details>

1. Run the container

    ```sh
    docker run --rm -it -v \
    /var/run/docker.sock:/var/run/docker.sock \
    -v /yourpath:/.config/jesseduffield/lazydocker \
    lazyteam/lazydocker
    ```

    - Don't forget to change `/yourpath` to an actual path you created to store lazydocker's config
    - You can also use this [docker-compose.yml](https://github.com/jesseduffield/lazydocker/blob/master/docker-compose.yml)
    - You might want to create an alias, for example:

        ```sh
        echo "alias lzd='docker run --rm -it -v /var/run/docker.sock:/var/run/docker.sock -v /yourpath/config:/.config/jesseduffield/lazydocker lazyteam/lazydocker'" >> ~/.zshrc
        ```



For development, you can build the image using:

```sh
git clone https://github.com/jesseduffield/lazydocker.git
cd lazydocker
docker build -t lazyteam/lazydocker \
    --build-arg BUILD_DATE=`date -u +"%Y-%m-%dT%H:%M:%SZ"` \
    --build-arg VCS_REF=`git rev-parse --short HEAD` \
    --build-arg VERSION=`git describe --abbrev=0 --tag` \
    .
```

If you encounter a compatibility issue with Docker bundled binary, try rebuilding
the image with the build argument `--build-arg DOCKER_VERSION="v$(docker -v | cut -d" " -f3 | rev | cut -c 2- | rev)"`
so that the bundled docker binary matches your host docker binary version.


## Usage

Call `lazydocker` in your terminal. I personally use this a lot so I've made an alias for it like so:

```
echo "alias lzd='lazydocker'" >> ~/.zshrc
```

(you can substitute .zshrc for whatever rc file you're using)

- Basic video tutorial [here](https://youtu.be/NICqQPxwJWw).
- List of keybindings
  [here](/docs/keybindings).

## Cool features

everything is one keypress away (or one click away! Mouse support FTW):

- viewing the state of your docker or docker-compose container environment at a glance
- viewing logs for a container/service
- viewing ascii graphs of your containers' metrics so that you can not only feel but also look like a developer
- customising those graphs to measure nearly any metric you want
- attaching to a container/service
- restarting/removing/rebuilding containers/services
- viewing the ancestor layers of a given image
- pruning containers, images, or volumes that are hogging up disk space

## Contributing

There is still a lot of work to go! Please check out the [contributing guide](CONTRIBUTING.md).
For contributor discussion about things not better discussed here in the repo, join the slack channel

[![Slack](/docs/resources/slack_rgb.png)](https://join.slack.com/t/lazydocker/shared_invite/enQtNjgwMjc0Njk3MzgwLTM0NThlMTZiZmNkNWJkY2VlYWYwZmY1NWYyYWViZmE0ZTcxMWZjMTFjNTU1ZTEwMDBiNWIxZTIxYzkwNDgyY2M)

## Donate

If you would like to support the development of lazydocker, consider [sponsoring me](https://github.com/sponsors/jesseduffield) (github is matching all donations dollar-for-dollar for 12 months)

## Social

If you want to see what I (Jesse) am up to in terms of development, follow me on
[twitter](https://twitter.com/DuffieldJesse) or watch me program on
[twitch](https://www.twitch.tv/jesseduffield)

## FAQ

### How do I edit my config?

By opening lazydocker, clicking on the 'project' panel in the top left, and pressing 'o' (or 'e' if your editor is vim). See [Config Docs](/docs/Config.md)

### How do I get text to wrap in my main panel?

In the future I want to make this the default, but for now there are some CPU issues that arise with wrapping. If you want to enable wrapping, use `gui.wrapMainPanel: true`

### How do you select text?

Because we support mouse events, you will need to hold option while dragging the mouse to indicate you're trying to select text rather than click on something. Alternatively you can disable mouse events via the `gui.ignoreMouseEvents` config value.

Mac Users: See [Issue #190](https://github.com/jesseduffield/lazydocker/issues/190) for other options.

### Why can't I see my container's logs?

By default we only show logs from the last hour, so that we're not putting too much strain on the machine. This may be why you can't see logs when you first start lazydocker. This can be overwritten in the config's `commandTemplates`

If you are running lazydocker in Docker container, it is a know bug, that you can't see logs or CPU usage.

## Alternatives

- [docui](https://github.com/skanehira/docui) - Skanehira beat me to the punch on making a docker terminal UI, so definitely check out that repo as well! I think the two repos can live in harmony though: lazydocker is more about managing existing containers/services, and docui is more about creating and configuring them.
- [Portainer](https://github.com/portainer/portainer) - Portainer tries to solve the same problem but it's accessed via your browser rather than your terminal. It also supports docker swarm.
- See [Awesome Docker list](https://github.com/veggiemonk/awesome-docker/blob/master/README.md#terminal) for similar tools to work with Docker.
