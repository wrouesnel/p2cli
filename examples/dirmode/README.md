# directory-mode example

Run this example with the following command:

```shell
./p2 -i content.yaml \
  -t root \
  -o out \
  --directory-mode
```

It is also possible to use `--tar` mode to emit  a tarfile bundle directly. 
In this mode, `-o` sets the prefix which output files are placed in the tarfile.

```shell
./p2 -i content.yaml \
  -t root \
  -o out \
  --tar out.tar \
  --directory-mode
```

