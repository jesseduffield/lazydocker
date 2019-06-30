# User Config:

## Default:

```
gui:
  scrollHeight: 2
  theme:
    activeBorderColor:
    - green
    - bold
    inactiveBorderColor:
    - white
    optionsTextColor:
    - blue
reporting: undetermined
commandTemplates:
  dockerCompose: docker-compose
  restartService: '{{ .DockerCompose }} restart {{ .Service.Name }}'
  stopService: '{{ .DockerCompose }} stop {{ .Service.Name }}'
  serviceLogs: '{{ .DockerCompose }} logs --since=60m --follow {{ .Service.Name }}'
  viewServiceLogs: '{{ .DockerCompose }} logs --follow {{ .Service.Name }}'
  rebuildService: '{{ .DockerCompose }} up -d --build {{ .Service.Name }}'
  recreateService: '{{ .DockerCompose }} up -d --force-recreate {{ .Service.Name }}'
  viewContainerLogs: docker logs --timestamps --follow --since=60m {{ .Container.ID
    }}
  containerLogs: docker logs --timestamps --follow --since=60m {{ .Container.ID }}
  allLogs: '{{ .DockerCompose }} logs --tail=300 --follow'
  viewAlLogs: '{{ .DockerCompose }} logs'
  dockerComposeConfig: '{{ .DockerCompose }} config'
  checkDockerComposeConfig: '{{ .DockerCompose }} config --quiet'
  serviceTop: '{{ .DockerCompose }} top {{ .Service.Name }}'
customCommands:
  containers:
  - name: bash
    attach: true
    command: docker exec -it {{ .Container.ID }} /bin/sh
    serviceNames: []
oS:
  openCommand: open {{filename}}
  openLinkCommand: open {{link}}
update:
  method: never
stats:
  graphs:
  - caption: CPU (%)
    statPath: DerivedStats.CPUPercentage
    color: blue
  - caption: Memory (%)
    statPath: DerivedStats.MemoryPercentage
    color: green
```

## To see what all of the config options mean, and what other options you can set, see https://godoc.org/github.com/jesseduffield/lazydocker/pkg/config

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
