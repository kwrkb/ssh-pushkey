# Generate: PATH="$(pwd)/demo:$PATH" vhs demo/demo.tape
Output demo/demo.gif

Require bash

Set Shell bash
Set FontSize 16
Set Width 800
Set Height 400
Set Theme "Catppuccin Mocha"
Set TypingSpeed 50ms

Sleep 500ms

Type "ssh-pushkey admin@192.168.1.10"
Enter
Sleep 1.5s

# Type password at the prompt
Type "MyP@ssw0rd"
Sleep 300ms
Enter

# Wait for all output to finish
Sleep 4s
