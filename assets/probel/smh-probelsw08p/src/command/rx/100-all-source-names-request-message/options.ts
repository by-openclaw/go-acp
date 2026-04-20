import { LengthOfNamesRequired } from '../../shared/length-of-names-required';

/**
 * AllSourceNamesRequestMessage command options
 *
 * @export
 * @interface AllSourceNamesRequestMessageCommandOptions
 */
export interface AllSourceNamesRequestMessageCommandOptions {
    /**
     * Length of Names Required.
     *
     * @type {LengthOfNamesRequired}
     * @memberof LengthOfNamesRequiredCommandOptions
     */
    lengthOfNames: LengthOfNamesRequired;
}

/**
 * Utility class providing some extra functionality on the AllSourceNamesRequestMessage command options
 *
 * @export
 * @class CommandOptionsUtility
 */
export class CommandOptionsUtility {
    /**
     * Gets a textual representation of the command options
     *
     * @param {AllSourceNamesRequestMessageCommandOptions} options the command options
     * @returns {string} the command options textual representation
     * @memberof CommandOptionsUtility
     */
    static toString(options: AllSourceNamesRequestMessageCommandOptions): string {
        return `lengthOfNames:${this.toStringLengthOfNamesRequired(options.lengthOfNames)}`;
    }

    /**
     * Gets a textual representation of the MaintenanceFunction
     *
     * @param {MaintenanceFunction} data the LengthOfNamesRequired
     * @returns {string} the LengthOfNamesRequired textual representation
     * @memberof CommandOptionsUtility
     */
    static toStringLengthOfNamesRequired(data: LengthOfNamesRequired): string {
        return `( ${data} - ${LengthOfNamesRequired[data]} )`;
    }
}
