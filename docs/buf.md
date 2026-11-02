Use the `buf` CLI for grpc generation.

Run through Nx: `npx nx run proto:fmt` formats protobuf files;
`npx nx run proto:lint` checks formatting and Buf lint rules without edits.
`npx nx run proto:verify` formats first, then checks. These also participate in
the workspace's language-agnostic `fmt`, `lint`, `verify`, and `fix` targets.
CI installs Buf 1.75.0 and runs these checks through `ocm-fleet:check`.

Dependencies should be local to the repository, or remote, for repeatable builds.

Follow the 1-1-1 Best Practice: https://protobuf.dev/best-practices/1-1-1/

And other Protobuf best practices: https://protobuf.dev/best-practices/dos-donts/

However, this should NOT supersede other API design guidance.
