output "vpc_id" {
    value = "${aws_subnet.test_subnet.vpc_id}"
}
output "subnet_id" {
    value = "${aws_subnet.test_subnet.id}"
}
output "subnet_cidr" {
    value = "${aws_subnet.test_subnet.cidr_block}"
}
output "env_name" {
    value = "${aws_subnet.test_subnet.tags.EnvName}"
}
output "tag_name" {
    value = "${aws_subnet.test_subnet.tags.Name}"
}
output "tag_hash" {
    value = "${aws_subnet.test_subnet.tags}"
}
output "tag_list" {
    value = "${values(aws_subnet.test_subnet.tags)}"
}
