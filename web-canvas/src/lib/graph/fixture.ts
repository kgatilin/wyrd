/**
 * Fixture UIGraph for testing the graph renderer without a running daemon.
 */
import type { UIGraph } from './types';

export const fixtureGraph: UIGraph = {
  schema: 'archai-uigraph-v1',
  boundedContexts: [
    { id: 'domain', name: 'Domain' },
  ],
  components: [
    {
      id: 'internal/domain/model',
      name: 'model',
      tech: 'Go',
      desc: 'Core domain models and value objects',
      bc: 'domain',
      internals: [
        {
          id: 'internal/domain/model.Package',
          kind: 'class',
          name: 'Package',
          exported: true,
          members: [
            { id: 'internal/domain/model.Package.ID', kind: 'prop', name: 'ID string', exported: true },
            { id: 'internal/domain/model.Package.Name', kind: 'prop', name: 'Name string', exported: true },
            { id: 'internal/domain/model.Package.Files', kind: 'prop', name: 'Files []File', exported: true },
          ],
        },
        {
          id: 'internal/domain/model.Component',
          kind: 'class',
          name: 'Component',
          exported: true,
          diff: 'changed',
          members: [
            { id: 'internal/domain/model.Component.ID', kind: 'prop', name: 'ID string', exported: true },
            { id: 'internal/domain/model.Component.Internals', kind: 'prop', name: 'Internals []Internal', exported: true, diff: 'added' },
          ],
        },
        {
          id: 'internal/domain/model.NewPackage',
          kind: 'func',
          name: 'NewPackage(id, name string) *Package',
          exported: true,
          members: [],
        },
      ],
      ports: [
        { id: 'internal/domain/model:out:Package', side: 'right', kind: 'out', name: 'Package' },
      ],
    },
    {
      id: 'internal/service',
      name: 'service',
      tech: 'Go',
      desc: 'Application services orchestrating domain operations',
      bc: 'domain',
      diff: 'added',
      internals: [
        {
          id: 'internal/service.GraphService',
          kind: 'iface',
          name: 'GraphService',
          exported: true,
          diff: 'added',
          members: [
            { id: 'internal/service.GraphService.Build', kind: 'method', name: 'Build(ctx context.Context, path string) (*Graph, error)', exported: true, diff: 'added' },
            { id: 'internal/service.GraphService.Query', kind: 'method', name: 'Query(ctx context.Context, q Query) ([]Node, error)', exported: true, diff: 'added' },
          ],
        },
        {
          id: 'internal/service.graphService',
          kind: 'class',
          name: 'graphService',
          exported: false,
          diff: 'added',
          members: [
            { id: 'internal/service.graphService.reader', kind: 'prop', name: 'reader Reader', exported: false, diff: 'added' },
            { id: 'internal/service.graphService.Build', kind: 'method', name: 'Build(ctx context.Context, path string) (*Graph, error)', exported: false, diff: 'added' },
          ],
        },
      ],
      ports: [
        { id: 'internal/service:in:model', side: 'left', kind: 'in', name: 'model' },
        { id: 'internal/service:out:GraphService', side: 'right', kind: 'out', name: 'GraphService' },
      ],
    },
    {
      id: 'internal/adapter/golang',
      name: 'golang',
      tech: 'Go',
      desc: 'Go source code parser adapter',
      bc: 'domain',
      internals: [
        {
          id: 'internal/adapter/golang.Reader',
          kind: 'class',
          name: 'Reader',
          exported: true,
          members: [
            { id: 'internal/adapter/golang.Reader.Read', kind: 'method', name: 'Read(ctx context.Context, paths []string) ([]Package, error)', exported: true },
          ],
        },
        {
          id: 'internal/adapter/golang.NewReader',
          kind: 'func',
          name: 'NewReader() *Reader',
          exported: true,
          members: [],
        },
      ],
      ports: [
        { id: 'internal/adapter/golang:out:Reader', side: 'right', kind: 'out', name: 'Reader' },
      ],
    },
    {
      id: 'internal/adapter/http',
      name: 'http',
      tech: 'Go - HTTP',
      desc: 'HTTP API handler',
      bc: 'domain',
      internals: [
        {
          id: 'internal/adapter/http.Handler',
          kind: 'class',
          name: 'Handler',
          exported: true,
          members: [
            { id: 'internal/adapter/http.Handler.ServeHTTP', kind: 'method', name: 'ServeHTTP(w http.ResponseWriter, r *http.Request)', exported: true },
          ],
        },
      ],
      ports: [
        { id: 'internal/adapter/http:in:GraphService', side: 'left', kind: 'in', name: 'GraphService' },
      ],
    },
    {
      id: 'cmd/archai',
      name: 'archai',
      tech: 'Go - CLI',
      desc: 'Command-line entry point',
      bc: 'domain',
      internals: [
        {
          id: 'cmd/archai.main',
          kind: 'func',
          name: 'main()',
          exported: false,
          members: [],
        },
      ],
      ports: [
        { id: 'cmd/archai:in:http', side: 'left', kind: 'in', name: 'http' },
      ],
    },
  ],
  edges: [
    {
      id: 'edge:model->service',
      from: 'internal/domain/model',
      to: 'internal/service',
      fromPort: 'internal/domain/model:out:Package',
      toPort: 'internal/service:in:model',
      label: 'uses',
    },
    {
      id: 'edge:service->http',
      from: 'internal/service',
      to: 'internal/adapter/http',
      fromPort: 'internal/service:out:GraphService',
      toPort: 'internal/adapter/http:in:GraphService',
      label: 'provides',
      diff: 'added',
    },
    {
      id: 'edge:http->cmd',
      from: 'internal/adapter/http',
      to: 'cmd/archai',
      fromPort: 'internal/adapter/http:out:Handler',
      toPort: 'cmd/archai:in:http',
      label: 'wires',
    },
    {
      id: 'edge:golang->service',
      from: 'internal/adapter/golang',
      to: 'internal/service',
      fromPort: 'internal/adapter/golang:out:Reader',
      toPort: 'internal/service:in:reader',
      label: 'implements',
    },
  ],
  relations: [
    {
      id: 'rel:service-impl-iface',
      kind: 'implements',
      fromComponentId: 'internal/service',
      fromInternalId: 'internal/service.graphService',
      toComponentId: 'internal/service',
      toInternalId: 'internal/service.GraphService',
      diff: 'added',
    },
  ],
};
