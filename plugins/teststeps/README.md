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
            user: root
            password: XXX
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
            user: root
            password: XXX
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
            user: root
            password: XXX
        options:
          timeout: 1m
        parameter:
          tool_path: /path/to/system-suite
          password: password
          cert_path: /path/to/cert
          option: bios option
          value: bios value
```

## ChipSec Teststep

The "ChipSec" teststep allows you to run different chipsec modules on your DUT.

**YAML Description**
```yaml
- name: chipsec
  label: Run ChipSec tests
  parameters:
    input: 
      - transport:
          proto: ssh                            # mandatory, type: string, options: local, ssh
          options:                              # mandatory when using ssh protocol
            host: TARGET_HOST                   # mandatory, type: string
            port: SSH_PORT                      # optional, type: integer, default: 22
            user: USERNAME                      # mandatory, type: string
            password: PASSWORD                  # optional, type: string
            identity_file: IDENTITY_FILE        # optional, type: string
        parameter:
            tool_path: TOOL_PATH                # optional, type: string
            modules: [MODULE1, MODULE2]         # optional, type: array of strings
            nix_os: NIXOS_FLAG                  # optional, type: boolean, default: false (tool_path is not required if set)
        options:
            timeout: TIMEOUT                    # optional, type: duration, default: 1m
```

**Example Usage**
```yaml
- name: chipsec
  label: Run ChipSec tests
  parameters:
    input: 
    - transport:
        proto: ssh
        options:
          host: "[[.Host]]"
          user: user
          password: password
      parameter:
        tool_path: /tmp/chipsec
        modules: [
          common.bios_kbrd_buffer, 
          common.bios_smi, 
          common.bios_ts, 
        ]
      options:
        timeout: 1m
```
## CPU Stats Teststep

The "CpuStats" teststep allows you to run check on different cpu stats of the DUT.

**YAML Description**
```yaml
- name: cpustats
  label: Run cpustats test
  parameters:
    input:                                      
      - transport:
          proto: ssh                            # mandatory, type: string, options: local, ssh
          options:                              # mandatory when using ssh protocol
            host: TARGET_HOST                   # mandatory, type: string
            port: SSH_PORT                      # optional, type: integer, default: 22
            user: USERNAME                      # mandatory, type: string
            password: PASSWORD                  # optional, type: string
            identity_file: IDENTITY_FILE        # optional, type: string
        parameter:
            tool_path: TOOL_PATH                # mandatory, type: string
            interval: INTERVAL                  # optional, type: string
        options:
            timeout: TIMEOUT                    # optional, type: duration, default: 1m | Must be higher than the interval
    expect:                                     # mandatory
      - general:                                # optional
          - option: OPTION                      # mandatory, type: string, options: CoresLogical, CoresPhysical, Profile, CurPowerConsumption, MaxPowerConsumption, PowerLimit1, PowerLimit2
          - value: VALUE                        # mandatory, type: string
      - individual:                             # optional
          - option: OPTION                      # mandatory, type: string, options: CStates, ScalingFrequency, CurrentFrequency, MinFrequency, MaxFrequency
          - value: VALUE                        # mandatory, type: string
```

**Example Usage**
```yaml
- name: cpustats
  label: Run CpuStats test
  parameters:
    input: 
    - transport:
        proto: ssh
        options:
          host: "[[.Host]]"
          user: user
          password: password
      parameter:
        tool_path: /tmp/system-suite
      options:
        timeout: 1m
      expect:
      - general:
          - option: CoresLogical
            value: "16"
          - option: CoresPhysical
            value: "12"
          - option: Profile
            value: balanced
      - individual:
          - core: 1
            option: CStates
            value: C1E,C6,C8,C10:<90;C1E<5,C8>10
          - core: 2
            option: CStates
            value: C1E,C6,C8,C10:<90
```

## CPU Load Teststep

The "CpuLoad" teststep allows you to put load on your DUT either the whole cpu or specific cores. You can also check CPU stats while the the cpu/cores working.

**YAML Description**
```yaml
- name: cpuload
  label: Run cpuload test
  parameters:
    input: 
      - transport:
          proto: ssh                            # mandatory, type: string, options: local, ssh
          options:                              # mandatory when using ssh protocol
            host: TARGET_HOST                   # mandatory, type: string
            port: SSH_PORT                      # optional, type: integer, default: 22
            user: USERNAME                      # mandatory, type: string
            password: PASSWORD                  # optional, type: string
            identity_file: IDENTITY_FILE        # optional, type: string
        parameter:
            tool_path: TOOL_PATH                # mandatory, type: string
            cores: [0,1,2,3]                    # optional, type: string
            duration: 30s                       # mandatory, type: string
        options:
            timeout: TIMEOUT                    # optional, type: duration, default: 1m | Must be higher than the duration
    expect:
      - general:
          - option: OPTION                      # mandatory, type: string, options: CoresLogical, CoresPhysical, Profile, CurPowerConsumption, MaxPowerConsumption, PowerLimit1, PowerLimit2
          - value: VALUE                        # mandatory, type: string
      - individual:
          - option: OPTION                      # mandatory, type: string, options: CStates, ScalingFrequency, CurrentFrequency, MinFrequency, MaxFrequency
          - value: VALUE                        # mandatory, type: string
```

**Example Usage**
```yaml
- name: cpuload
  label: Run CpuLoad test
  parameters:
    input: 
    - transport:
        proto: ssh
        options:
          host: "[[.Host]]"
          user: user
          password: password
      parameter:
        tool_path: /tmp/system-suite
        cores: [0,1,2,3,4,5]
        duration: 30m
      options:
        timeout: 1m
```

## CPU Set Teststep

The "CpuSet" teststep allows you to set cpu cores on or off.

**YAML Description**
```yaml
- name: cpuset
  label: Run cpuset test
  parameters:
    input: 
      - transport:
          proto: ssh                            # mandatory, type: string, options: local, ssh
          options:                              # mandatory when using ssh protocol
            host: TARGET_HOST                   # mandatory, type: string
            port: SSH_PORT                      # optional, type: integer, default: 22
            user: USERNAME                      # mandatory, type: string
            password: PASSWORD                  # optional, type: string
            identity_file: IDENTITY_FILE        # optional, type: string
        parameter:
            tool_path: TOOL_PATH                # mandatory, type: string
            command: COMMAND                    # mandatory, type: string, options: core
            cores: [0,1,2,3]                    # mandatory, type: string
            args: [activate]                    # mandatory, type: string, options: activate, deactivate
        options:
            timeout: TIMEOUT                    # optional, type: duration, default: 1m
```

**Example Usage**
```yaml
- name: cpuload
  label: Run CpuLoad test
  parameters:
    input: 
    - transport:
        proto: ssh
        options:
          host: "[[.Host]]"
          user: user
          password: password
      parameter:
        command: core
        tool_path: /tmp/system-suite
        cores: [0,1,2,3,4,5]
        args: [deactivate]
      options:
        timeout: 1m
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
## DutCtl Teststep

The "dutctl" teststep allows you to control a device of your choide. Power, Flash and Serial commands are supported.

**YAML Description**
```yaml
- name: dutctl
  label: dutctl teststep
  parameters:
    input: 
      - parameter:
            host: TARGET_HOST               # mandatory, type: string
            command: COMMAND_NAME           # mandatory, type: string
            args: [ARG1, ARG2, ARG3]        # optional, type: array of strings
            input: INPUT_STRING             # opitonal, type: string
        options:
            timeout: TIMEOUT                # optional, type: duration, default: 1m
    expect: 
      - regex: EXPECTED_REGEX1              # opitonal, type: string
      - regex: EXPECTED_REGEX2              # opitonal, type: string
```

**Example Usage**
```yaml
- name: dutctl
  label: dutctl teststep
  parameters:
    input:
    - parameter:
        host: 192.168.1.100
        command: power
        args: [on]
        input: "user\n"
      options:
        timeout: 2m
    expect:
    - regex: searchedString
    - regex: (everypossibleregex)
```

## FWTS Teststep

The "FWTS" teststep allows you to run the Firmware Testsuite on your DUT.

**YAML Description**
```yaml
- name: fwts
  label: Run fwts tests
  parameters:
    input: 
      - transport:
          proto: ssh                            # mandatory, type: string, options: local, ssh
          options:                              # mandatory when using ssh protocol
            host: TARGET_HOST                   # mandatory, type: string
            port: SSH_PORT                      # optional, type: integer, default: 22
            user: USERNAME                      # mandatory, type: string
            password: PASSWORD                  # optional, type: string
            identity_file: IDENTITY_FILE        # optional, type: string
        parameter:
            flags: [FLAG1, FLAG2]               # optional, type: array of strings
        options:
            timeout: TIMEOUT                    # optional, type: duration, default: 1m
```

**Example Usage**
```yaml
- name: fwts
  label: Run fwts tests
  parameters:
    input: 
    - transport:
        proto: ssh
        options:
          host: "[[.Host]]"
          user: user
          password: password
      parameter:
        flags: [-b]
      options:
        timeout: 1m
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
