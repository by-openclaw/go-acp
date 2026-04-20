export class Constants {
    static readonly ABSTRACT_UTILITY_CLASS_CTOR_ERROR_MSG = 'Abstract Utility class cannot be instantiated';

    // // Out of range Commands params
    static readonly MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG =
        'MatrixId argument is out of range ! Allowed range value [0, 255]';
    static readonly LEVEL_IS_OUT_OF_RANGE_ERROR_MSG = 'Level argument is out of range ! Allowed range value [0, 255]';
    static readonly SOURCE_IS_OUT_OF_RANGE_ERROR_MSG =
        'Source argument is out of range ! Allowed range value [0, 65535]';
    static readonly DESTINATION_IS_OUT_OF_RANGE_ERROR_MSG =
        'Destination argument is out of range ! Allowed range value [0, 65535]';
    static readonly DEVICE_IS_OUT_OF_RANGE_ERROR_MSG =
        'Device argument is out of range ! Allowed range value [0, 1023]';
    static readonly SALVO_IS_OUT_OF_RANGE_ERROR_MSG = 'Salvo argument is out of range ! Allowed range value [0, 127]';
    static readonly CONNECT_INDEX_IS_OUT_OF_RANGE_ERROR_MSG =
        'Connect Index argument is out of range ! Allowed range value [0, 65535]';

    static readonly SOURCE_NAMES_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG =
        'Source Items argument is out of range ! Allowed range value [0, 65535]';
    static readonly SOURCE_ID_AND_MAXIMUM_NUMBER_OF_NAMES_IS_OUT_OF_RANGE_ERROR_MSG =
        'SourceId and Maximum Number of Names are out of range ! Allowed range value [0, 65535]';
    static readonly SALVO_GROUP_MESSAGE_COMMAND_IS_OUT_OF_RANGE_ERROR_MSG =
        'Salvo Group Message argument is out of range !';

    static readonly DESTINATION_MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG =
        'destinationMatrixId argument is out of range ! Allowed range value [0, 19]';
    static readonly DESTINATION_ASSOCIATION_IS_OUT_OF_RANGE_ERROR_MSG =
        'firstDestinationAssociationId argument is out of range ! Allowed range value [0, 65535]';
    static readonly DESTINATION_ASSOCIATION_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG =
        'Destination Association Items argument is out of range !';
    // isExtended Command
    static readonly EXTENDED_GENERAL_COMMAND_RECEIVED_INFO_MSG = 'Extended General Command Received - ';
    static readonly GENERAL_COMMAND_RECEIVED_INFO_MSG = 'General Command Received - ';
    static readonly EXTENDED_GENERAL_COMMAND_TRANSMITTED_INFO_MSG = 'Extended General Command Transmitted - ';
    static readonly GENERAL_COMMAND_TRANSMITTED_INFO_MSG = 'General Command Transmitted - ';
    static readonly GENERATE_COMMAND_ERROR_MSG = 'Could not generate the command -';

    /**
     * Buffer.utility errors message
     * + combine2BytesMsbLsb(msbOf: number, lsbOf: number)
     *
     * @static
     * @memberof Constants
     */
    static readonly COMBINE_2_BYTES_MSB_IS_OUT_OF_RANGE_ERROR_MSG =
        '"msbOf" argument is out of range ! Allowed range value [0, 15]';
    /**
     * Buffer.utility errors message
     * + combine2BytesMsbLsb(msbOf: number, lsbOf: number)
     *
     * @static
     * @memberof Constants
     */
    static readonly COMBINE_2_BYTES_LSB_IS_OUT_OF_RANGE_ERROR_MSG =
        '"lsbOf" argument is out of range ! Allowed range value [0, 15]';
    /**
     * Buffer.utility errors message
     * + combine2BytesMultiplierMsbLsb(msbOf: number, lsbOf: number)
     *
     * @static
     * @memberof Constants
     */
    static readonly COMBINE_2_BYTES_MULTIPLIER_MSB_IS_OUT_OF_RANGE_ERROR_MSG =
        '"msbOf" argument is out of range ! Allowed range value [0, 895]';
    /**
     * Buffer.utility errors message
     * + combine2BytesMultiplierMsbLsb(msbOf: number, lsbOf: number)
     *
     * @static
     * @memberof Constants
     */
    static readonly COMBINE_2_BYTES_MULTIPLIER_LSB_IS_OUT_OF_RANGE_ERROR_MSG =
        '"lsbOf" argument is out of range ! Allowed range value [0, 895]';
}
