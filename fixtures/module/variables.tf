variable "access_key" {}
variable "secret_key" {}
variable "region" {
    default = "us-east-1"
}
variable "vpc_id" {}
variable "subnet_cidr" {}
variable "tag_name" {
    default = "terraform-resource-module-test"
}
variable "env_name" {}
