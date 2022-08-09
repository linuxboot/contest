# Variables plugin

The *variables* plugin adds its input parameters as step plugin variables that could be later referred to by other plugins.

## Parameters

Any parameter should be a single-value parameter that will be added as a test-variable.
For example:

{
    "name": "variables",
    "label": "variablesstep"
    "parameters": {
        "string_variable": ["Hello"],
        "int_variable": [123],
        "complex_variable": [{"hello": "world"}]
    }
}

Will generate a string, int and a json-object variables respectively.

These parameters could be later accessed in the following manner in accordance with step variables guidance:

{
    "name": "cmd",
    "label": "cmdstep",
    "parameters": { 
        "executable": [echo],
        "args": ["{{ StringVar \"variablesstep.string_variable\" }} world number {{ IntVar \"variablesstep.int_variable\" }}"],
        "emit_stdout": [true],
        "emit_stderr": [true]
    }
}
