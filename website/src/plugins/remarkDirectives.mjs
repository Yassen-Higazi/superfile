import { visit } from "unist-util-visit";

/**
 * Converts remark-directive container nodes (:::tip,

:::caution,

:::note)
 * into <Callout> components that docs.css can style.
 */
export function remarkDirectivesHandler() {
	return tree => {
		visit(tree, node => {
			if (node.type === "containerDirective" || node.type === "leafDirective") {
				const name = node.name; // "tip" | "caution" | "note" | "danger"
				node.type = "mdxJsxFlowElement";
				node.name = "Callout";
				node.attributes = [
					...Object.entries(node.attributes ?? {}).map(([attributeName, value]) => ({
						type: "mdxJsxAttribute",
						name: attributeName,
						value
					})),
					{
						type: "mdxJsxAttribute",
						name: "type",
						value: name
					}
				];
			}
		});
	};
}
