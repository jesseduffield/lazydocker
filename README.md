<p align="center">
  <img src="https://user-images.githubusercontent.com/8456633/59972109-8e9c8480-95cc-11e9-8350-38f7f86ba76d.png">
</p>

A simple terminal UI for both docker and docker-compose, written in Go with the [gocui](https://github.com/jroimartin/gocui 'gocui') library.

[![CircleCI](https://circleci.com/gh/jesseduffield/lazydocker.svg?style=svg)](https://circleci.com/gh/jesseduffield/lazydocker) [![Go Report Card](https://goreportcard.com/badge/github.com/jesseduffield/lazydocker)](https://goreportcard.com/report/github.com/jesseduffield/lazydocker) [![GolangCI](https://golangci.com/badges/github.com/jesseduffield/lazydocker.svg)](https://golangci.com) [![GoDoc](https://godoc.org/github.com/jesseduffield/lazydocker?status.svg)](http://godoc.org/github.com/jesseduffield/lazydocker) [![GitHub tag](https://img.shields.io/github/tag/jesseduffield/lazydocker.svg)]()



![Gif](/docs/resources/demo3.gif)

[Demo](https://youtu.be/NICqQPxwJWw)

Minor rant incoming: Something's not working? Maybe a service is down. `docker-compose ps`. Yep, it's that microservice that's still buggy. No issue, I'll just restart it: `docker-compose restart`. Okay now let's try again. Oh wait the issue is still there. Hmm. `docker-compose ps`. Right so the service must have just stopped immediately after starting. I probably would have known that if I was reading the log stream, but there is a lot of clutter in there from other services. I could get the logs for just that one service with `docker compose logs --follow myservice` but that dies everytime the service dies so I'd need to run that command every time I restart the service. I could alternatively run `docker-compose up myservice` and in that terminal window if the service is down I could just `up` it again, but now I've got one service hogging a terminal window even after I no longer care about its logs. I guess when I want to reclaim the terminal realestate I can do `ctrl+P,Q`, but... wait, that's not working for some reason. Should I use ctrl+C instead? I can't remember if that closes the foreground process or kills the actual service.

What a headache!

Memorising docker commands is hard. Memorising aliases is slightly less hard. Keeping track of your containers across multiple terminal windows is near impossible. What if you had all the information you needed in one terminal window with every common command living one keypress away (and the ability to add custom commands as well). Lazydocker's goal is to make that dream a reality.

- [Installation](https://github.com/jesseduffield/lazydocker#installation)
- [Usage](https://github.com/jesseduffield/lazydocker#usage),
  [Keybindings](/docs/keybindings)
- [Cool Features](https://github.com/jesseduffield/lazydocker#cool-features)
- [Contributing](https://github.com/jesseduffield/lazydocker#contributing)
- [Video Tutorial](https://youtu.be/NICqQPxwJWw)
- [Config Docs](/docs/Config.md)
- [Twitch Stream](https://www.twitch.tv/jesseduffield)

## Installation

### Homebrew

```sh
brew tap jesseduffield/lazydocker
brew install lazydocker
```

### Binary Release (Linux/OSX)

You can download a binary release [here](https://github.com/jesseduffield/lazydocker/releases).

### Go

required go version: 1.12

```sh
go get github.com/jesseduffield/lazydocker
```

### Arch Linux AUR

You can install lazydocker using your AUR package manager of choice or by running:

```sh
git clone https://aur.archlinux.org/lazydocker.git ~/lazydocker
cd ~/lazydocker
makepkg --install
```

A development version of the AUR package is also [available](https://aur.archlinux.org/lazydocker-git.git)

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

If you would like to support the development of lazydocker, please donate

[![Donate](https://d1iczxrky3cnb2.cloudfront.net/button-medium-blue.png)](https://donorbox.org/lazydocker)

## Social

If you want to see what I (Jesse) am up to in terms of development, follow me on
[twitter](https://twitter.com/DuffieldJesse) or watch me program on
[twitch](https://www.twitch.tv/jesseduffield)

## Alternatives

- [docui](https://github.com/skanehira/docui) - Skanehira beat me to the punch on making a docker terminal UI, so definitely check out that repo as well! I think the two repos can live in harmony though: lazydocker is more about managing existing containers/services, and docui is more about creating and configuring them.
- [Portainer](https://github.com/portainer/portainer) - Portainer tries to solve the same problem but it's accessed via your browser rather than your terminal. It also supports docker swarm.
