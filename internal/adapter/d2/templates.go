package d2

// classesTemplate returns the D2 classes block for reusable styles.
func classesTemplate(style StyleConfig) string {
	style = style.withDefaults()
	return `classes: {
  ` + ClassDomain + `: {
    style.fill: "` + style.Domain.ContainerFill + `"
    style.font-color: "` + style.Domain.ContainerFontColor + `"
  }
  ` + ClassService + `: {
    style.fill: "` + style.Service.ContainerFill + `"
    style.font-color: "` + style.Service.ContainerFontColor + `"
  }
  ` + ClassFactory + `: {
    style.fill: "` + style.Factory.ContainerFill + `"
    style.font-color: "` + style.Factory.ContainerFontColor + `"
  }
  ` + ClassValue + `: {
    style.fill: "` + style.Value.ContainerFill + `"
    style.font-color: "` + style.Value.ContainerFontColor + `"
  }
  ` + ClassDomainSymbol + `: {
    style.fill: "` + style.Domain.ClassFill + `"
    style.font-color: "` + style.Domain.ClassFontColor + `"
  }
  ` + ClassServiceSymbol + `: {
    style.fill: "` + style.Service.ClassFill + `"
    style.font-color: "` + style.Service.ClassFontColor + `"
  }
  ` + ClassFactorySymbol + `: {
    style.fill: "` + style.Factory.ClassFill + `"
    style.font-color: "` + style.Factory.ClassFontColor + `"
  }
  ` + ClassValueSymbol + `: {
    style.fill: "` + style.Value.ClassFill + `"
    style.font-color: "` + style.Value.ClassFontColor + `"
  }
}
`
}

// legendTemplate returns the D2 legend block content.
// The legend is positioned at top-right and shows the DDD color coding.
func legendTemplate(style StyleConfig) string {
	style = style.withDefaults()
	return `legend: {
  label: "Color Legend (DDD)"
  style.stroke: "` + style.Legend.Stroke + `"
  style.fill: "` + style.Legend.Fill + `"
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
