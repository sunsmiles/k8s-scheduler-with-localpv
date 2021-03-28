/**
 * @Author: ss
 * @Email: sunflyers@163.com
 * @Description:
 * @File:  main_test.go
 * @Version: 1.0.0
 * @Date: 2021/03/28 8:40 PM
 */

package main

import (
	"k8s.io/client-go/kubernetes"
	"reflect"
	"testing"
)

func Test_createKubernetesClient(t *testing.T) {
	tests := []struct {
		name    string
		want    *kubernetes.Clientset
		wantErr bool
	}{
		// TODO: Add test cases.
		{
			"test create k8s client",
			nil,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := createKubernetesClient()
			if (err != nil) != tt.wantErr {
				t.Errorf("createKubernetesClient() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("createKubernetesClient() got = %v, want %v", got, tt.want)
			}
		})
	}
}
