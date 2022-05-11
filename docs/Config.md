# User Config:

## Opening The User Config

The location of the user config will differ depending on your OS. You can open it via lazydocker by opening the application, clicking on the 'project' panel at the top left and pressing 'o' (or pressing 'e' if your files open in vim).

Changes to the user config will only take place after closing and re-opening lazydocker

### Locations:

- OSX: `~/Library/Application Support/jesseduffield/lazydocker/config.yml`
- Linux: `~/.config/lazydocker/config.yml`
- Windows: `C:\\Users\\<User>\\AppData\\Roaming\\jesseduffield\\lazydocker\\config.yml` (I think)

## Default:

```yml
gui:
  scrollHeight: 2
  language: 'auto' # one of 'auto' | 'en' | 'pl' | 'nl' | 'de' | 'tr'
  theme:
    activeBorderColor:
      - green
      - bold
    inactiveBorderColor:
      - white
    optionsTextColor:
      - blue
  returnImmediately: false
  wrapMainPanel: true
logs:
  timestamps: false
  since: '60m'
commandTemplates:
  dockerCompose: docker-compose
  restartService: '{{ .DockerCompose }} restart {{ .Service.Name }}'
  startService: '{{ .DockerCompose }} start {{ .Service.Name }}'
  stopService: '{{ .DockerCompose }} stop {{ .Service.Name }}'
  serviceLogs: '{{ .DockerCompose }} logs --since=60m --follow {{ .Service.Name }}'
  viewServiceLogs: '{{ .DockerCompose }} logs --follow {{ .Service.Name }}'
  rebuildService: '{{ .DockerCompose }} up -d --build {{ .Service.Name }}'
  recreateService: '{{ .DockerCompose }} up -d --force-recreate {{ .Service.Name }}'
  viewContainerLogs: docker logs --follow --since=60m {{ .Container.ID
    }}
  allLogs: '{{ .DockerCompose }} logs --tail=300 --follow'
  viewAlLogs: '{{ .DockerCompose }} logs'
  dockerComposeConfig: '{{ .DockerCompose }} config'
  checkDockerComposeConfig: '{{ .DockerCompose }} config --quiet'
  serviceTop: '{{ .DockerCompose }} top {{ .Service.Name }}'
oS:
  openCommand: open {{filename}}
  openLinkCommand: open {{link}}
stats:
  graphs:
    - caption: CPU (%)
      statPath: DerivedStats.CPUPercentage
      color: blue
    - caption: Memory (%)
      statPath: DerivedStats.MemoryPercentage
      color: green
```

## To see what all of the config options mean, and what other options you can set, see [here](https://godoc.org/github.com/jesseduffield/lazydocker/pkg/config)

## Color Attributes:

For color attributes you can choose an array of attributes (with max one color attribute)
The available attributes are:

- default
- black
- red
- green
- yellow
- blue
- magenta
- cyan
- white
- bold
- reverse # useful for high-contrast
- underline

## Custom Commands

You can add custom commands like so:

```yaml
customCommands:
  containers:
    - name: bash
      attach: true
      command: 'docker exec -it {{ .Container.ID }} bash'
      serviceNames: []
```
