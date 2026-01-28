package d2

// classesTemplate returns the D2 classes block for reusable styles.
func classesTemplate() string {
	return `classes: {
  ` + ClassDomain + `: {
    style.fill: "` + ColorBlue + `"
    style.font-color: "#000"
  }
  ` + ClassService + `: {
    style.fill: "` + ColorPurple + `"
    style.font-color: "#000"
  }
  ` + ClassFactory + `: {
    style.fill: "` + ColorGreen + `"
    style.font-color: "#000"
  }
  ` + ClassValue + `: {
    style.fill: "` + ColorGray + `"
    style.font-color: "#000"
  }
}
`
}

// legendTemplate returns the D2 legend block content.
// The legend is positioned at top-right and shows the DDD color coding.
func legendTemplate() string {
	return `legend: {
  label: "Color Legend (DDD)"
  style.stroke: "` + ColorLegendBorder + `"
  style.fill: "` + ColorLegendFill + `"
  near: top-right

  aggregate: {
    label: "Domain Model"
    shape: class
    class: ` + ClassDomain + `
  }
  service: {
    label: "Service"
    shape: class
    class: ` + ClassService + `
  }
  factory: {
    label: "Factory"
    shape: class
    class: ` + ClassFactory + `
  }
  options: {
    label: "Value Object"
    shape: class
    class: ` + ClassValue + `
  }
}`
}
