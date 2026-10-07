// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package globalserviceexport

var templateInline = `{
  "$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#",
  "contentVersion": "1.0.0.0",
  "metadata": {
    "_generator": {
      "name": "bicep",
      "version": "0.38.33.27573",
      "templateHash": "16882031058036640307"
    }
  },
  "parameters": {
    "name": {
      "type": "string",
      "defaultValue": "foo"
    },
    "backends": {
      "type": "array",
      "defaultValue": []
    }
  },
  "variables": {
    "copy": [
      {
        "name": "backendObjs",
        "count": "[length(parameters('backends'))]",
        "input": {
          "name": "[last(split(parameters('backends')[copyIndex('backendObjs')], '/'))]",
          "properties": {
            "loadBalancerFrontendIPConfiguration": {
              "id": "[parameters('backends')[copyIndex('backendObjs')]]"
            }
          }
        }
      }
    ],
    "loadBalancers_mitchs_global_lb_name": "[format('{0}-global-lb', parameters('name'))]",
    "publicIPAddresses_mitchs_global_ip_name": "[format('{0}-global-ip', parameters('name'))]"
  },
  "resources": [
    {
      "type": "Microsoft.Network/loadBalancers/backendAddressPools",
      "apiVersion": "2024-07-01",
      "name": "[format('{0}/{1}', variables('loadBalancers_mitchs_global_lb_name'), 'kubernetes-mc')]",
      "properties": {
        "loadBalancerBackendAddresses": "[variables('backendObjs')]"
      },
      "dependsOn": [
        "[resourceId('Microsoft.Network/loadBalancers', variables('loadBalancers_mitchs_global_lb_name'))]"
      ]
    },
    {
      "type": "Microsoft.Network/publicIPAddresses",
      "apiVersion": "2024-07-01",
      "name": "[variables('publicIPAddresses_mitchs_global_ip_name')]",
      "location": "eastus2",
      "sku": {
        "name": "Standard",
        "tier": "Global"
      },
      "properties": {
        "publicIPAddressVersion": "IPv4",
        "publicIPAllocationMethod": "Static"
      }
    },
    {
      "type": "Microsoft.Network/loadBalancers",
      "apiVersion": "2024-07-01",
      "name": "[variables('loadBalancers_mitchs_global_lb_name')]",
      "location": "eastus2",
      "sku": {
        "name": "Standard",
        "tier": "Global"
      },
      "properties": {
        "frontendIPConfigurations": [
          {
            "name": "mitchs-global-ip-config",
            "properties": {
              "publicIPAddress": {
                "id": "[resourceId('Microsoft.Network/publicIPAddresses', variables('publicIPAddresses_mitchs_global_ip_name'))]"
              }
            }
          }
        ],
        "backendAddressPools": [
          {
            "name": "kubernetes-mc"
          }
        ],
        "loadBalancingRules": [
          {
            "name": "tcp-80-k8s2",
            "properties": {
              "frontendIPConfiguration": {
                "id": "[resourceId('Microsoft.Network/loadBalancers/frontendIPConfigurations', variables('loadBalancers_mitchs_global_lb_name'), 'mitchs-global-ip-config')]"
              },
              "frontendPort": 80,
              "backendPort": 80,
              "enableFloatingIP": true,
              "idleTimeoutInMinutes": 4,
              "protocol": "Tcp",
              "backendAddressPools": [
                {
                  "id": "[resourceId('Microsoft.Network/loadBalancers/backendAddressPools', variables('loadBalancers_mitchs_global_lb_name'), 'kubernetes-mc')]"
                }
              ]
            }
          }
        ]
      },
      "dependsOn": [
        "[resourceId('Microsoft.Network/publicIPAddresses', variables('publicIPAddresses_mitchs_global_ip_name'))]"
      ]
    }
  ],
  "outputs": {
    "publicGlobalIPAddress": {
      "type": "string",
      "value": "[reference(resourceId('Microsoft.Network/publicIPAddresses', variables('publicIPAddresses_mitchs_global_ip_name'))).ipAddress]"
    }
  }
}
`
