# User Config:

## Opening The User Config

The location of the user config will differ depending on your OS. You can open it via lazydocker by opening the application, clicking on the 'project' panel at the top left and pressing 'o' (or pressing 'e' if your files open in vim).

Changes to the user config will only take place after closing and re-opening lazydocker

### Locations:

- OSX: `~/Library/Application Support/jesseduffield/lazydocker/config.yml`
- Linux: `~/.config/lazydocker/config.yml`
- Windows: `C:\Users\<User>\AppData\Roaming\lazydocker\config.yml`

JSON schema is available for `config.yml` so that IntelliSense in Visual Studio Code
(completion and error checking) is automatically enabled when the [YAML Red Hat][yaml]
extension is installed. However, note that automatic schema detection only works
if your config file is in one of the standard paths mentioned above. If you
override the path to the file, you can still make IntelliSense work by adding

```yaml
# yaml-language-server: $schema=https://json.schemastore.org/lazydocker.json
```

to the top of your config file or via [Visual Studio Code settings.json config][settings].

[yaml]: https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml
[settings]: https://github.com/redhat-developer/vscode-yaml#associating-a-schema-to-a-glob-pattern-via-yamlschemas

## Default:

```yml
gui:
  scrollHeight: 2
  language: "auto" # one of 'auto' | 'en' | 'pl' | 'nl' | 'de' | 'tr'
  border: "rounded" # one of 'rounded' | 'single' | 'double' | 'hidden'
  theme:
    activeBorderColor:
      - green
      - bold
    inactiveBorderColor:
      - white
    selectedLineBgColor:
      - blue
    optionsTextColor:
      - blue
  returnImmediately: false
  wrapMainPanel: true
  # Side panel width as a ratio of the screen's width
  sidePanelWidth: 0.333
  # Determines whether we show the bottom line (the one containing keybinding
  # info and the status of the app).
  showBottomLine: true
  # When true, increases vertical space used by focused side panel,
  # creating an accordion effect
  expandFocusedSidePanel: false
  # Determines which screen mode will be used on startup
  screenMode: "normal" # one of 'normal' | 'half' | 'fullscreen'
  # Determines the style of the container status and container health display in the
  # containers panel. "long": full words (default), "short": one or two characters,
  # "icon": unicode emoji.
  containerStatusHealthStyle: "long"
logs:
  timestamps: false
  since: '60m' # set to '' to show all logs
  tail: '' # set to 200 to show last 200 lines of logs
tls:
  enable: false # Set to true to enable TLS connections to Docker daemon
  caCertPath: "" # Path to Certificate Authority (CA) certificate file
  certPath: "" # Path to client certificate file for mutual TLS authentication
  keyPath: "" # Path to client private key file
  host: "" # Hostname or IP for certificate validation (must match server certificate)
  insecureSkipVerify: false # Skip certificate verification (NOT recommended for production)
commandTemplates:
  dockerCompose: docker compose # Determines the Docker Compose command to run, referred to as .DockerCompose in commandTemplates
  restartService: '{{ .DockerCompose }} restart {{ .Service.Name }}'
  up:  '{{ .DockerCompose }} up -d'
  down: '{{ .DockerCompose }} down'
  downWithVolumes: '{{ .DockerCompose }} down --volumes'
  upService:  '{{ .DockerCompose }} up -d {{ .Service.Name }}'
  startService: '{{ .DockerCompose }} start {{ .Service.Name }}'
  stopService: '{{ .DockerCompose }} stop {{ .Service.Name }}'
  serviceLogs: '{{ .DockerCompose }} logs --since=60m --follow {{ .Service.Name }}'
  viewServiceLogs: '{{ .DockerCompose }} logs --follow {{ .Service.Name }}'
  rebuildService: '{{ .DockerCompose }} up -d --build {{ .Service.Name }}'
  recreateService: '{{ .DockerCompose }} up -d --force-recreate {{ .Service.Name }}'
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

## TLS Configuration

Lazydocker supports secure connections to Docker daemon using TLS (Transport Layer Security). This is essential when connecting to remote Docker daemons or when your Docker daemon requires client certificate authentication.

### Configuration Options

The `tls` section in your config supports the following options:

- **`enable`**: Boolean flag to enable/disable TLS connections
- **`caCertPath`**: Path to the Certificate Authority (CA) certificate file used to verify the Docker daemon's certificate
- **`certPath`**: Path to the client certificate file for mutual TLS authentication
- **`keyPath`**: Path to the client private key file corresponding to the client certificate
- **`host`**: The hostname or IP address expected on the Docker daemon's certificate. This must match the Common Name (CN) or a Subject Alternative Name (SAN) in the server's certificate
- **`insecureSkipVerify`**: When set to `true`, skips certificate verification. **Only use this for testing - it's insecure for production!**

### Basic Example

```yaml
tls:
  enable: true
  caCertPath: "/home/user/.docker/ca.pem"
  certPath: "/home/user/.docker/cert.pem"
  keyPath: "/home/user/.docker/key.pem"
  host: "docker.example.com"
  insecureSkipVerify: false
```

### Remote Docker Daemon Setup

When connecting to a remote Docker daemon with TLS:

1. **Set the Docker Host**: Use the `DOCKER_HOST` environment variable to specify where to connect:
   ```bash
   export DOCKER_HOST=tcp://docker.example.com:2376
   ```

2. **Configure TLS**: Update your `config.yml`:
   ```yaml
   tls:
     enable: true
     caCertPath: "/path/to/ca.pem"
     certPath: "/path/to/cert.pem"
     keyPath: "/path/to/key.pem"
     host: "docker.example.com"  # Must match certificate
     insecureSkipVerify: false
   ```

### Important Notes

1. **Don't confuse two different "hosts"**:
   - `DOCKER_HOST` environment variable: **WHERE** to connect (e.g., `tcp://192.168.1.100:2376`)
   - `tls.host` config field: **WHAT NAME** to expect on the certificate (e.g., `docker.example.com`)

2. **Certificate Validation**: The `host` field must match a name in your Docker daemon's certificate. You can check your certificate's valid names with:
   ```bash
   openssl x509 -in server-cert.pem -noout -text
   ```

3. **Certificate Paths**: Ensure all certificate files are readable by the user running Lazydocker

### Testing TLS Configuration

For testing purposes only, you can skip certificate verification:

```yaml
tls:
  enable: true
  caCertPath: "/path/to/ca.pem"
  certPath: "/path/to/cert.pem" 
  keyPath: "/path/to/key.pem"
  host: "any-name-here"  # Ignored when insecureSkipVerify is true
  insecureSkipVerify: true  # INSECURE - only for testing!
```

### Common TLS Errors

- **"x509: certificate is valid for X, not Y"**: The `host` field doesn't match any name on the server's certificate
- **"certificate signed by unknown authority"**: The `caCertPath` is incorrect or the server's certificate wasn't signed by the specified CA
- **"no such host"**: Network connectivity issue with `DOCKER_HOST` (not a TLS config problem)
- **"tls: failed to verify certificate"**: General certificate validation failure - check all certificate paths and the `host` field

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

You may use the following go templates (such as `{{ .Container.ID }}` above) in your commands:
- `{{ .DockerCompose }}`: the docker compose command (default: `docker-compose`)
- [`{{ .Container }}`](https://pkg.go.dev/github.com/jesseduffield/lazydocker@v0.20.0/pkg/commands#Container) and its fields. For example: `{{ .Container.Container.ImageID }}`
- [`{{ .Service }}`](https://pkg.go.dev/github.com/jesseduffield/lazydocker@v0.20.0/pkg/commands#Service) and its fields. For example: `{{ .Service.Name }}`

## Replacements

You can add replacements like so:

```yaml
replacements:
  imageNamePrefixes:
    '123456789012.dkr.ecr.us-east-1.amazonaws.com': '<prod>'
    '923456789999.dkr.ecr.us-east-1.amazonaws.com': '<dev>'
```
