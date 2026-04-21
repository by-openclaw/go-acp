import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandOptionsUtility, SingleSourceAssociationNamesRequestMessageCommandOptions } from './options';
import { CommandParamsUtility, SingleSourceAssociationNamesRequestMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Single Source Association Names Request Message command
 *
 * This message is issued by the remote device to request the name for a single source association for a given matrix.
 * The controller will respond with one SOURCE ASSOCIATION NAMES RESPONSE message (Command Byte 116).
 *
 * + Command issued by the remote device
 * @export
 * @class SingleSourceAssociationNamesRequestMessageCommand
 * @extends {CommandBase<SingleSourceAssociationNamesRequestMessageCommandParams>}
 */
export class SingleSourceAssociationNamesRequestMessageCommand extends CommandBase<
    SingleSourceAssociationNamesRequestMessageCommandParams,
    SingleSourceAssociationNamesRequestMessageCommandOptions
> {
    /**
     * Creates an instance of SingleSourceAssociationNamesRequestMessageCommand
     *
     * @param {SingleSourceAssociationNamesRequestMessageCommandParams} params the command parameters
     * @param {SingleSourceAssociationNamesRequestMessageCommandOptions} _options the command options
     * @memberof SingleSourceAssociationNamesRequestMessageCommand
     */
    constructor(
        params: SingleSourceAssociationNamesRequestMessageCommandParams,
        options: SingleSourceAssociationNamesRequestMessageCommandOptions
    ) {
        super(CommandIdentifiers.RX.GENERAL.SINGLE_SOURCE_ASSOCIATION_NAMES_REQUEST_MESSAGE, params, options);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params, options (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof SingleSourceAssociationNamesRequestMessageCommand
     */
    toLogDescription(): string {
        return `General - ${CommandParamsUtility.toString(this.params)}, ${CommandOptionsUtility.toString(
            this.options
        )}`;
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof SingleSourceAssociationNamesRequestMessageCommand
     */
    protected buildData(): Buffer {
        return this.buildDataNormal();
    }

    /**
     * Build the general command
     * Returns Probel SW-P-08 - General Single Source Association Names Request Message CMD_115_0X73
     *
     * + This message is issued by the remote device to request the name for a single source association for a given matrix.
     * + The controller will respond with one SOURCE ASSOCIATION NAMES RESPONSE message (Command Byte 116).
     *
     * | Message | Command Byte | 115 - 0x73                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | Matrix                                                                                                                             |
     * |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     * |         | Bits[0-3]    | 0                                                                                                                                  |
     * | Byte 2  |Name Length   | Length of Names Required                                                                                                           |
     * |         | 0            | 4 char names                                                                                                                       |
     * |         | 1            | 8 char names                                                                                                                       |
     * |         | 2            | 12 char names                                                                                                                      |
     * | Byte 3  | Src multiplier| Source number DIV 256                                                                                                             |
     * | Byte 4  | Src  number  | Source number MOD 256                                                                                                              |
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof SingleSourceAssociationNamesRequestMessageCommand
     */
    protected buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 5 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, 0))
            .writeUInt8(this.options.lengthOfNames)
            .writeUInt8(Math.floor(this.params.sourceId / 256))
            .writeUInt8(this.params.sourceId % 256)
            .toBuffer();
    }

    /**
     * Validate the parameters, options and throw a ValidationError in case of error
     *
     * @private
     * @param {SingleSourceAssociationNamesRequestMessageCommandParams} params the command parameters
     * @param {LengthOfNamesRequiredCommandOptions} options the command options
     * @memberof SingleSourceAssociationNamesRequestMessageCommand
     */
    private validateParams(params: SingleSourceAssociationNamesRequestMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = {
            ...new CommandParamsValidator(params).validate()
        };

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }
}
