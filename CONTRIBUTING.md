# Contributing

Thank you for considering contributing to HC!

## Reporting Issues

If you find a bug or have a feature request, please open an issue on [GitHub](https://github.com/morkid/hc/issues).

## Development

1. Fork the repository.
2. Create a feature branch (`git checkout -b feature/my-feature`).
3. Run tests to ensure everything is working:

```bash
go test -cover ./...
```

4. Commit your changes (`git commit -am 'Add my feature'`).
5. Push to the branch (`git push origin feature/my-feature`).
6. Open a Pull Request.

## Code Style

- Run `go fmt` before committing.
- Ensure all tests pass.
- Maintain **100% code coverage** — write tests for any new or changed code.
- Run `go test -cover ./...` to verify coverage before committing.

## License

By contributing, you agree that your contributions will be licensed under the [MIT License](LICENSE).
