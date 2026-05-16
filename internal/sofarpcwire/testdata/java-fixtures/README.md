# Java SOFARPC Wire Fixtures

This project generates Java-produced `response-content` fixtures and verifies
Go-produced `request-content` fixtures with the real SOFARPC/Hessian runtime.

The default SOFARPC baseline is defined in `pom.xml` as `sofarpc.version`.
Override it for company/project compatibility checks:

```sh
mvn -q -Dsofarpc.version=5.4.0 package
mvn -q -Dsofarpc.version=5.4.0 dependency:build-classpath -Dmdep.outputFile=target/classpath.txt
java -cp "target/classes:$(cat target/classpath.txt)" com.example.WireFixtureVerifier ../golden
```

To regenerate the committed baseline fixtures:

```sh
go run ../go-fixtures
mvn -q package
mvn -q dependency:build-classpath -Dmdep.outputFile=target/classpath.txt
java -cp "target/classes:$(cat target/classpath.txt)" com.example.WireFixtureGenerator ../golden
java -cp "target/classes:$(cat target/classpath.txt)" com.example.WireFixtureVerifier ../golden
```

For a version matrix, generate into a temporary directory, run both Java and Go
checks against that directory, and keep only the baseline fixtures committed.
