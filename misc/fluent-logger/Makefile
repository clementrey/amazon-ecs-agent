# Copyright Amazon.com Inc. or its affiliates. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License"). You may
# not use this file except in compliance with the License. A copy of the
# License is located at
#
#	http://aws.amazon.com/apache2.0/
#
# or in the "license" file accompanying this file. This file is distributed
# on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
# express or implied. See the License for the specific language governing
# permissions and limitations under the License.
.PHONY: all clean fluent-logger image

all: fluent-logger image

fluent-logger: fluent-logger.go
	@./build-in-docker

image: fluent-logger
	docker build -t amazon/fluent-logger:make .

clean:
	rm -f fluent-logger
	-docker rmi -f "amazon/fluent-logger:make"