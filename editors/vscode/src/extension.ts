import * as path from "path";
import { workspace, ExtensionContext } from "vscode";
import {
  LanguageClient,
  LanguageClientOptions,
  ServerOptions,
} from "vscode-languageclient/node";

let client: LanguageClient;

export function activate(context: ExtensionContext) {
  // Find the ease binary — check workspace setting, then PATH
  const config = workspace.getConfiguration("ease");
  const easePath = config.get<string>("serverPath") || "ease";

  const serverOptions: ServerOptions = {
    command: easePath,
    args: ["lsp"],
  };

  const clientOptions: LanguageClientOptions = {
    documentSelector: [{ scheme: "file", language: "ease" }],
  };

  client = new LanguageClient(
    "ease-lsp",
    "Ease Language Server",
    serverOptions,
    clientOptions
  );

  client.start();
}

export function deactivate(): Thenable<void> | undefined {
  if (!client) {
    return undefined;
  }
  return client.stop();
}
