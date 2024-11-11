# Acceptance Tests

## Feature testing map
<!-- insert snippet -->
### Feature: Schema CRDs

|                 SCENARIO                 | EKS | AKS | GKE | K3D |
|------------------------------------------|-----|-----|-----|-----|
| Manage product catalog schema (Protobuf) |     |     |     | ✅  |
| Manage order event schema (JSON)         |     |     |     | ✅  |
| Manage customer profile schema (Avro)    |     |     |     | ✅  |


### Feature: Topic CRDs

|   SCENARIO    | EKS | AKS | GKE | K3D |
|---------------|-----|-----|-----|-----|
| Manage topics |     |     |     | ✅  |


### Feature: User CRDs

|             SCENARIO             | EKS | AKS | GKE | K3D |
|----------------------------------|-----|-----|-----|-----|
| Manage users                     |     |     |     | ✅  |
| Manage authentication-only users |     |     |     | ✅  |
| Manage authorization-only users  |     |     |     | ✅  |

