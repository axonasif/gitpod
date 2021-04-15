/**
 * Copyright (c) 2020 Gitpod GmbH. All rights reserved.
 * Licensed under the MIT License. See License-MIT.txt in the project root for license information.
 */


resource "google_compute_network" "gitpod" {
  name                    = "gitpod"
  description             = "Gitpod Cluster Network"
  auto_create_subnetworks = false
  project                 = var.project
}

module "kubernetes" {
  source = "../../modules/kubernetes"

  name    = "gitpod"
  network = google_compute_network.gitpod.name
  project = var.project
  region  = var.region
}

module "kubeconfig" {
  source = "../../modules/kubeconfig"

  cluster = {
    name = "gitpod"
  }

  depends_on = [
    module.kubernetes
  ]
}

module "dns" {
  source = "../../modules/dns"

  project   = var.project
  region    = var.region
  zone_name = var.zone_name
  name      = "gitpod-dns"

  providers = {
    google     = google
    kubernetes = kubernetes
  }
}

module "certmanager" {
  source = "../../modules/certmanager"

  project = var.project
  email   = var.certificate_email
  domain  = module.dns.hostname

  providers = {
    google     = google
    kubernetes = kubernetes
    kubectl    = kubectl
  }
}

module "registry" {
  source = "../../modules/registry"

  project  = var.project
  location = var.container_registry.location

  providers = {
    google     = google
    kubernetes = kubernetes
  }
}

module "storage" {
  source = "../../modules/storage"

  project  = var.project
  region   = var.region
  location = "EU"
}

module "database" {
  source = "../../modules/database"

  project = var.project
  name    = "db"
  region  = var.region
  network = {
    id   = google_compute_network.gitpod.id
    name = google_compute_network.gitpod.name
  }
}

#
# Gitpod
#

output "values" {
  value = yamlencode(
      merge(
        yamldecode(file("./values.static.yaml")),
        try(yamldecode(module.dns.values), {}),
        try(yamldecode(module.certmanager.values), {}),
        try(yamldecode(module.registry.values), {}),
        try(yamldecode(module.storage.values), {}),
        try(yamldecode(module.database.values), {})
      )
    )
}
