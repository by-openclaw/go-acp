import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { IValidator } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CrossPointConnectOnGoSalvoGroupMessageCommandParams } from './params';

/**
 * CrossPointConnectOnGoSalvoGroupMessage command parameters validator
 *
 * @export
 * @class CommandParamsValidator
 * @implements {IValidator<CrossPointConnectOnGoSalvoGroupMessageCommandParams>}
 */
export class CommandParamsValidator implements IValidator<CrossPointConnectOnGoSalvoGroupMessageCommandParams> {
    /**
    /**
     * Creates an instance of CommandParamsValidator
     *
     * @param {CrossPointConnectOnGoSalvoGroupMessageCommandParams} data the command data
     * @memberof CommandParamsValidator
     */
    constructor(readonly data: CrossPointConnectOnGoSalvoGroupMessageCommandParams) {}

    /**
     * Validates the command parameter(s) and returns one error message for each invalid parameter
     *
     * @returns {(Record<string, LocaleData>)} the localized validation errors
     * @memberof CommandParamsValidator
     */
    validate(): Record<string, LocaleData> {
        const errors: Record<string, any> = {};
        const cache = LocaleDataCache.INSTANCE;

        // if (this.isMatrixIdOutOfRange()) {
        //     errors.matrixId = cache.getCommandErrorLocaleData(CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG);
        // }
        // if (this.isLevelIdOutOfRange()) {
        //     errors.levelId = cache.getCommandErrorLocaleData(CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG);
        // }
        // if (this.isDestinationIdOutOfRange()) {
        //     errors.destinationId = cache.getCommandErrorLocaleData(
        //         CommandErrorsKeys.DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG
        //     );
        // }
        // if (this.isSourceIdOutOfRange()) {
        //     errors.sourceId = cache.getCommandErrorLocaleData(CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG);
        // }
        // if (this.isSalvoIdOutOfRange()) {
        //     errors.salvoId = cache.getCommandErrorLocaleData(CommandErrorsKeys.SALVO_IS_OUT_OF_RANGE_ERROR_MSG);
        // }
        // TODO: GLOBAL OUT OF RANGE
        if (this.isgroupMessageCommandItemsOutOfRange()) {
            errors.salvoGroupMessageCommand = cache.getCommandErrorLocaleData(
                CommandErrorsKeys.SALVO_GROUP_MESSAGE_COMMAND_IS_OUT_OF_RANGE_ERROR_MSG
            );
        }
        return errors;
    }

    // /**
    //  * Gets a boolean indicating whether the matrixId is out of range
    //  * + false = matrixId is included within the limits
    //  * + true = matrixId [0-255] is out of range
    //  *
    //  * @private
    //  * @returns {boolean} 'true' if matrixId is out of range; otherwise 'false'
    //  * @memberof CommandParamsValidator
    //  */
    // private isMatrixIdOutOfRange(): boolean {
    //     for (let byteIndex = 0; byteIndex < this.data.salvoGroupMessageCommandItems.length; byteIndex++) {
    //         const data = this.data.salvoGroupMessageCommandItems[byteIndex];
    //         if (data.matrixId < 0 || data.matrixId > 255) {
    //             return true;
    //         }
    //     }
    //     return false;
    // }

    // /**
    //  * Gets a boolean indicating whether the  levelId is out of range
    //  * + false = levelId is included within the limits
    //  * + true = levelId [0-255] is out of range
    //  *
    //  * @private
    //  * @returns {boolean} 'true' if levelId is out of range; otherwise 'false'
    //  * @memberof CommandParamsValidator
    //  */
    // private isLevelIdOutOfRange(): boolean {
    //     for (let byteIndex = 0; byteIndex < this.data.salvoGroupMessageCommandItems.length; byteIndex++) {
    //         const data = this.data.salvoGroupMessageCommandItems[byteIndex];
    //         if (data.levelId < 0 || data.levelId > 255) {
    //             return true;
    //         }
    //     }
    //     return false;
    // }

    // /**
    //  * Gets a boolean indicating whether the  destinationId is out of range
    //  * + false = destinationId is included within the limits
    //  * + true = destinationId  [0-65535] is out of range
    //  *
    //  * @private
    //  * @returns {boolean} 'true' if destinationId is out of range; otherwise 'false'
    //  * @memberof CommandParamsValidator
    //  */
    // private isDestinationIdOutOfRange(): boolean {
    //     for (let byteIndex = 0; byteIndex < this.data.salvoGroupMessageCommandItems.length; byteIndex++) {
    //         const data = this.data.salvoGroupMessageCommandItems[byteIndex];
    //         if (data.destinationId < 0 || data.destinationId > 65535) {
    //             return true;
    //         }
    //     }
    //     return false;
    // }

    // /**
    //  * Gets a boolean indicating whether the  sourceId is out of range
    //  * + false = sourceId is included within the limits
    //  * + true = sourceId  [0-65535] is out of range
    //  *
    //  * @private
    //  * @returns {boolean} 'true' if sourceId is out of range; otherwise 'false'
    //  * @memberof CommandParamsValidator
    //  */
    // private isSourceIdOutOfRange(): boolean {
    //     for (let byteIndex = 0; byteIndex < this.data.salvoGroupMessageCommandItems.length; byteIndex++) {
    //         const data = this.data.salvoGroupMessageCommandItems[byteIndex];
    //         if (data.sourceId < 0 || data.sourceId > 65535) {
    //             return true;
    //         }
    //     }
    //     return false;
    // }

    // /**
    //  * Gets a boolean indicating whether the  salvoId is out of range
    //  * + false = salvoId is included within the limits
    //  * + true = salvoId [0-127] is out of range
    //  *
    //  * @private
    //  * @returns {boolean} 'true' if salvoId is out of range; otherwise 'false'
    //  * @memberof CommandParamsValidator
    //  */
    // private isSalvoIdOutOfRange(): boolean {
    //     for (let byteIndex = 0; byteIndex < this.data.salvoGroupMessageCommandItems.length; byteIndex++) {
    //         const data = this.data.salvoGroupMessageCommandItems[byteIndex];
    //         if (data.salvoId < 0 || data.salvoId > 127) {
    //             return true;
    //         }
    //     }
    //     return false;
    // }

    // TODO: To refactor if we want to split the out of range per items instead of global
    /**
     * Gets a boolean indicating whether the matrixId | levelId | destinationId | sourceId | salvoId is out of range
     * + false = matrixId | levelId | destinationId | sourceId | salvoId is included within the limits
     * + true = matrixId | levelId | destinationId | sourceId | salvoId is out of range
     *
     * @private
     * @returns {boolean} 'true' if matrixId | levelId | destinationId | sourceId | salvoId is out of range; otherwise 'false'
     * @memberof CommandParamsValidator
     */
    private isgroupMessageCommandItemsOutOfRange(): boolean {
        if (this.data.salvoGroupMessageCommandItems.length === 0) {
            return true;
        }
        for (let byteIndex = 0; byteIndex < this.data.salvoGroupMessageCommandItems.length; byteIndex++) {
            const data = this.data.salvoGroupMessageCommandItems[byteIndex];
            if (data.matrixId < 0 || data.matrixId > 255) {
                return true;
            }
            if (data.levelId < 0 || data.levelId > 255) {
                return true;
            }
            if (data.destinationId < 0 || data.destinationId > 65535) {
                return true;
            }
            if (data.sourceId < 0 || data.sourceId > 65535) {
                return true;
            }
            if (data.salvoId < 0 || data.salvoId > 127) {
                return true;
            }
        }
        return false;
    }
}
