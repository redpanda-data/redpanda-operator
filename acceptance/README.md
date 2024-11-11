# Acceptance Tests

## Feature testing map
<!-- insert snippet -->
### Feature: Schema CRDs

|                  SCENARIO                  | EKS | AKS | GKE | K3D |
|--------------------------------------------|-----|-----|-----|-----|
| Managing customer profile schema (Avro)    |     |     |     | ✅  |
| Managing product catalog schema (Protobuf) |     |     |     | ✅  |
| Managing order event schema (JSON)         |     |     |     | ✅  |


### Feature: Topic CRDs

|    SCENARIO     | EKS | AKS | GKE | K3D |
|-----------------|-----|-----|-----|-----|
| Managing Topics |     |     |     | ✅  |


### Feature: User CRDs

|             SCENARIO             | EKS | AKS | GKE | K3D |
|----------------------------------|-----|-----|-----|-----|
| Manage users                     |     |     |     | ✅  |
| Manage authentication-only users |     |     |     | ✅  |
| Manage authorization-only users  |     |     |     | ✅  |

