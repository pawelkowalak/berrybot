package main

import (
	"testing"

	pb "github.com/pawelkowalak/berrybot/proto"
)

func TestClassifyDirection(t *testing.T) {
	tests := []struct {
		name string
		dx   int32
		dy   int32
		want driveCmd
	}{
		{
			name: "stop in dead zone",
			dx:   0,
			dy:   0,
			want: cmdStop,
		},
		{
			name: "forward",
			dx:   0,
			dy:   driveDeadZone + 1,
			want: cmdForward,
		},
		{
			name: "backward",
			dx:   0,
			dy:   -(driveDeadZone + 1),
			want: cmdBackward,
		},
		{
			name: "sharp right",
			dx:   driveDeadZone + 1,
			dy:   0,
			want: cmdSharpRight,
		},
		{
			name: "sharp left",
			dx:   -(driveDeadZone + 1),
			dy:   0,
			want: cmdSharpLeft,
		},
		{
			name: "forward right",
			dx:   driveDeadZone + 1,
			dy:   driveDeadZone + 1,
			want: cmdFwdRight,
		},
		{
			name: "forward left",
			dx:   -(driveDeadZone + 1),
			dy:   driveDeadZone + 1,
			want: cmdFwdLeft,
		},
		{
			name: "backward right",
			dx:   driveDeadZone + 1,
			dy:   -(driveDeadZone + 1),
			want: cmdBackRight,
		},
		{
			name: "backward left",
			dx:   -(driveDeadZone + 1),
			dy:   -(driveDeadZone + 1),
			want: cmdBackLeft,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got := classifyDirection(&pb.Direction{Dx: tt.dx, Dy: tt.dy})
			if got != tt.want {
				t.Fatalf("classifyDirection(%d,%d) = %v, want %v", tt.dx, tt.dy, got, tt.want)
			}
		})
	}
}
