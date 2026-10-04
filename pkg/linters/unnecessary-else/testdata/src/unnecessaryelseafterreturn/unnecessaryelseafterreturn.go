package unnecessaryelseafterreturn

// BadSimpleElseAfterReturn should be flagged
func BadSimpleElseAfterReturn(x int) string {
	if x > 0 {
		return "positive"
	} else { // want `else block is unnecessary; remove it since the preceding if always returns`
		return "non-positive"
	}
}

// GoodNoElse should not be flagged
func GoodNoElse(x int) string {
	if x > 0 {
		return "positive"
	}
	return "non-positive"
}

// GoodIfBodyDoesntReturn should not be flagged
func GoodIfBodyDoesntReturn(x int) string {
	if x > 0 {
		println("positive")
	} else {
		return "non-positive"
	}
	return "zero"
}

// BadNestedIfWithElse should be flagged
func BadNestedIfWithElse(x int, y int) string {
	if x > 0 {
		if y > 0 {
			return "both positive"
		} else { // want `else block is unnecessary; remove it since the preceding if always returns`
			return "x positive, y not"
		}
		return ""
	} else { // want `else block is unnecessary; remove it since the preceding if always returns`
		return "x not positive"
	}
}

// BadElseWithSwitch should be flagged - the else body always returns via switch with fallthrough
func BadElseWithSwitch(x int) string {
	if x > 0 {
		return "positive"
	} else { // want `else block is unnecessary; remove it since the preceding if always returns`
		switch x {
		case 0:
			return "zero"
		case -1:
			return "negative one"
		default:
			return "other"
		}
	}
}
