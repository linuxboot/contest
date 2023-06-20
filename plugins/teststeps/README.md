# Teststep Documentation

Templating in the test description yaml files is supported. The delimiter for templating is [[]]. So templating works like this: "[[.TEMPLATE]]". The templating has to be in quote marks.

## BIOS Certificate Teststep

The "BIOS Certificate" teststep allows you to enable, update or disable BIOS certificates for authentication.

**YAML Description**
```yaml
- name: bios certificate management
  label: bios certificate teststep
  parameters:
    input: 
      - transport:
          proto: ssh                        # mandatory, type: string, options: local, ssh
          options:                          # mandatory when using ssh protocol
            host: TARGET_HOST               # mandatory, type: string
            port: SSH_PORT                  # optional, type: integer, default: 22
            user: USERNAME                  # mandatory, type: string
            password: PASSWORD              # optional, type: string
            identity_file: IDENTITY_FILE    # optional, type: string
        parameter:
            tool_path: TOOL_PATH            # optional, type: string
            password: PASSWORD              # optional, type: string
            cert_path: CERTIFICATE_PATH     # optional, type: string
            key_path: KEY_PATH              # optional, type: string
        options:
            timeout: TIMEOUT                # optional, type: duration, default: 1m
```

**Example Usage**
```yaml
- name: bios certificate management
  label: bios certificate teststep
    parameters:
      input: 
      - command: enable
        transport:
          proto: ssh
          options:
            host: 192.168.1.100
            user: secunettest
            password: testsecunet
        options:
          timeout: 1m
        parameter:
          tool_path: /path/to/system-suite
          password: password
          cert_path: /path/to/cert
```

## BIOS Get Teststep

The "BIOS Get" teststep allows you to get BIOS settings and expect values.

**YAML Description**
```yaml
- name: get bios setting
  label: get bios setting teststep
  parameters:
    input: 
      - transport:
          proto: ssh                        # mandatory, type: string, options: local, ssh
          options:                          # mandatory when using ssh protocol
            host: TARGET_HOST               # mandatory, type: string
            port: SSH_PORT                  # optional, type: integer, default: 22
            user: USERNAME                  # mandatory, type: string
            password: PASSWORD              # optional, type: string
            identity_file: IDENTITY_FILE    # optional, type: string
        parameter:
            tool_path: TOOL_PATH            # optional, type: string
        options:
            timeout: TIMEOUT                # optional, type: duration, default: 1m
    expect: 
      - option: option1                     # optional, type: string
        value: value1                       # optional, type: string
      - option: option2                     # optional, type: string
        value: value2                       # optional, type: string
```

**Example Usage**
```yaml
- name: get bios setting
  label: get bios setting teststep
    parameters:
      input: 
      - transport:
          proto: ssh
          options:
            host: 192.168.1.100
            user: secunettest
            password: testsecunet
        options:
          timeout: 1m
        parameter:
          tool_path: /path/to/system-suite
      expect: 
      - option: IsEnabled
        value: "true"
```

## BIOS Set Teststep

The "BIOS Set" teststep allows you to set BIOS settings with a specific value.

**YAML Description**
```yaml
- name: set bios setting
  label: set bios setting teststep
  parameters:
    input: 
      - transport:
          proto: ssh                        # mandatory, type: string, options: local, ssh
          options:                          # mandatory when using ssh protocol
            host: TARGET_HOST               # mandatory, type: string
            port: SSH_PORT                  # optional, type: integer, default: 22
            user: USERNAME                  # mandatory, type: string
            password: PASSWORD              # optional, type: string
            identity_file: IDENTITY_FILE    # optional, type: string
        parameter:
            tool_path: TOOL_PATH            # optional, type: string
            password: PASSWORD              # optional, type: string
            key_path: KEY_PATH              # optional, type: string
            option: BIOS_OPTION             # optional, type: string
            value: BIOS_VALUE               # optional, type: string
            shall_fail: SHALL_FAIL          # optional, type: boolean
        options:
            timeout: TIMEOUT                # optional, type: duration, default: 1m
```

**Example Usage**
```yaml
- name: bios certificate
  label: bios certificate teststep
    parameters:
      input: 
      - transport:
          proto: ssh
          options:
            host: 192.168.1.100
            user: secunettest
            password: testsecunet
        options:
          timeout: 1m
        parameter:
          tool_path: /path/to/system-suite
          password: password
          cert_path: /path/to/cert
          option: bios option
          value: bios value
```

## Copy Teststep

The "copy" teststep allows you to copy files or directories to a destination locally or on a target device using SSH protocol.

**YAML Description**
```yaml
- name: copy
  label: copy teststep
  parameters:
    input: 
      - transport:
          proto: ssh                        # mandatory, type: string, options: local, ssh
          options:                          # mandatory when using ssh protocol
            host: TARGET_HOST               # mandatory, type: string
            port: SSH_PORT                  # optional, type: integer, default: 22
            user: USERNAME                  # mandatory, type: string
            password: PASSWORD              # optional, type: string
            identity_file: IDENTITY_FILE    # optional, type: string
        parameter:
            source: SOURCE_PATH             # mandatory, type: string
            destination: DESTINATION_PATH   # mandatory, type: string
            recursive: RECURSIVE_FLAG       # optional, type: boolean, default: false
        options:
            timeout: TIMEOUT                # optional, type: duration, default: 1m
```

**Example Usage**
```yaml
- name: copy
  label: copy teststep
  parameters:
    input:
    - transport:
        proto: ssh
        options:
          host: 192.168.1.100
          port: 2222
          user: admin
          identity_file: /path/to/identity/file
      parameter:
        source: /path/to/files
        destination: /home/user/files
        recursive: true
      options:
        timeout: 2m
```

## HWaaS Teststep

The "hwaas" teststep allows you to control a DUT via the HWaaS API.

**YAML Description**
```yaml
- name: hwaas
  label: hwaas teststep
  parameters:
    input: 
      - parameter:
            host: API_HOST                  # mandatory, type: string
            port: PORT                      # optional, type: int, default 8080
            context_id: CONTEXTID           # optional, type: string, default ULID
            machine_id: MACHINEID           # optional, type: string, default machine
            device_id: DEVICEID             # optional, type: string, default device
            command: EXEUTABLE              # optional, type: string, options: power, flash
            args: [ARG1, ARG2, ARG3]        # optional, type: array of strings
        options:
            timeout: TIMEOUT                # optional, type: duration, default: 1m
```

**Example Usage**
```yaml
- name: hwaas
  label: hwaas teststep
  parameters:
    input: 
    - parameter:
        host: 192.168.1.100
        command: power
        args: [on]
      options:
        timeout: 1m
```

## Ping Teststep

The "copy" teststep allows you to copy files or directories to a destination locally or on a target device using SSH protocol.

**YAML Description**
```yaml
- name: ping
  label: ping teststep
  parameters:
    input: 
      - parameter:
            host: TARGET_HOST               # mandatory, type: string
            port: PORT                      # optional, type: int, default 22
        options:
            timeout: TIMEOUT                # optional, type: duration, default: 1m
```

**Example Usage**
```yaml
- name: ping
  label: ping teststep
  parameters:
    input:
    - transport:
        proto: ssh
        options:
          host: 192.168.1.100
          port: 2222
          user: admin
          identity_file: /path/to/identity/file
      parameter:
        source: /path/to/files
        destination: /home/user/files
        recursive: true
      options:
        timeout: 2m
```


## SSHCMD Teststep

The "sshcmd" teststep allows you to execute binaries locally or on a target device using SSH protocol.

**YAML Description**
```yaml
- name: sshcmd
  label: sshcmd teststep
  parameters:
    input: 
      - transport:
          proto: ssh                        # mandatory, type: string, options: local, ssh
          options:                          # mandatory when using ssh protocol
            host: TARGET_HOST               # mandatory, type: string
            port: SSH_PORT                  # optional, type: integer, default: 22
            user: USERNAME                  # mandatory, type: string
            password: PASSWORD              # optional, type: string
            identity_file: IDENTITY_FILE    # optional, type: string
        binary:
            executable: EXECUTABLE_PATH     # mandatory, type: string
            args: [ARG1, ARG2, ARG3]        # optional, type: array of strings
        options:
            timeout: TIMEOUT                # optional, type: duration, default: 1m
```

**Example Usage**
```yaml
- name: sshcmd
  label: sshcmd teststep
  parameters:
    input:
    - transport:
        proto: ssh
        options:
          host: 192.168.1.100
          port: 2222
          user: admin
          identity_file: /path/to/identity/file
      bianry:
        executable: /path/to/exe
        args: [argument, argument, argument]
        recursive: true
      options:
        timeout: 2m
```
